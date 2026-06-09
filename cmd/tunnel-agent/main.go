package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"qwernot/tunnel-control/internal/core"
	"qwernot/tunnel-control/internal/integrated"
)

type syncBundle struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Node        core.Node     `json:"node"`
	Users       []core.User   `json:"users"`
	Tunnels     []core.Tunnel `json:"tunnels"`
}

type applyResponse struct {
	Status  string                    `json:"status"`
	Clients []integrated.ClientStatus `json:"clients"`
}

func main() {
	var (
		controlURL   = flag.String("control-url", getenvAllowEmpty("CONTROL_URL", "http://127.0.0.1:8088"), "central tunnel-control URL")
		nodeID       = flag.String("node-id", getenv("NODE_ID", ""), "node id registered in central control")
		nodeToken    = flag.String("node-token", getenv("NODE_TOKEN", ""), "node token registered in central control")
		dataDir      = flag.String("data-dir", getenv("DATA_DIR", "/app/data"), "agent data directory")
		syncInterval = flag.Duration("sync-interval", 10*time.Second, "configuration sync interval")
		apiAddr      = flag.String("api-addr", getenv("AGENT_API_ADDR", ":18089"), "agent push API listen address")
		npsPort      = flag.Int("nps-port", getenvInt("NPS_BRIDGE_PORT", 18024), "nps bridge port")
		npsTLSPort   = flag.Int("nps-tls-port", getenvInt("NPS_TLS_BRIDGE_PORT", 18025), "nps TLS bridge port")
		npsHTTPPort  = flag.Int("nps-http-port", getenvInt("NPS_HTTP_PORT", 9080), "nps HTTP proxy port")
		npsHTTPSPort = flag.Int("nps-https-port", getenvInt("NPS_HTTPS_PORT", 9443), "nps HTTPS proxy port")
	)
	flag.Parse()
	if *nodeID == "" || *nodeToken == "" {
		log.Fatal("NODE_ID and NODE_TOKEN are required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	npsWorkDir := filepath.Join(*dataDir, "nps")
	if err := prepareAgentData(*dataDir, npsWorkDir, *nodeToken, *npsPort, *npsTLSPort, *npsHTTPPort, *npsHTTPSPort); err != nil {
		log.Fatal(err)
	}
	var syncMu sync.Mutex

	go func() {
		err := integrated.RunNPS(ctx, integrated.NPSOptions{
			WorkDir:    npsWorkDir,
			BridgePort: *npsPort,
		})
		log.Printf("nps stopped: %v", err)
	}()
	go serveApplyAPI(*apiAddr, *nodeID, *nodeToken, &syncMu)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if strings.TrimSpace(*controlURL) == "" {
					continue
				}
				if err := reportTrafficOnce(*controlURL, *nodeID, *nodeToken); err != nil {
					log.Printf("report traffic failed: %v", err)
				}
			}
		}
	}()

	time.Sleep(2 * time.Second)
	if strings.TrimSpace(*controlURL) == "" {
		select {}
	}
	ticker := time.NewTicker(*syncInterval)
	defer ticker.Stop()
	for {
		if err := syncOnce(*controlURL, *nodeID, *nodeToken, &syncMu); err != nil {
			log.Printf("sync failed: %v", err)
		}
		<-ticker.C
	}
}

func syncOnce(controlURL, nodeID, nodeToken string, syncMu *sync.Mutex) error {
	req, err := http.NewRequest(http.MethodGet, controlURL+"/api/agent/bundle?node="+nodeID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+nodeToken)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("central returned %s", resp.Status)
	}
	var bundle syncBundle
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		return err
	}
	return applyBundle(nodeID, bundle, syncMu)
}

func serveApplyAPI(addr, nodeID, nodeToken string, syncMu *sync.Mutex) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/agent/apply", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" || token != nodeToken {
			http.Error(w, "invalid node token", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var bundle syncBundle
		if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := applyBundle(nodeID, bundle, syncMu); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(applyResponse{
			Status:  "ok",
			Clients: runtimeClientStatuses(nodeID),
		})
	})
	log.Printf("agent push API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("agent push API stopped: %v", err)
	}
}

func runtimeClientStatuses(nodeID string) []integrated.ClientStatus {
	statuses := integrated.NPSClientStatuses()
	for i := range statuses {
		statuses[i].NodeID = nodeID
	}
	return statuses
}

func applyBundle(nodeID string, bundle syncBundle, syncMu *sync.Mutex) error {
	if bundle.Node.ID != "" && bundle.Node.ID != nodeID {
		return fmt.Errorf("bundle is for node %q, expected %q", bundle.Node.ID, nodeID)
	}
	syncMu.Lock()
	defer syncMu.Unlock()
	if err := integrated.SyncNPSState(bundle.Users, bundle.Tunnels); err != nil {
		return err
	}
	log.Printf("synced node=%s users=%d tunnels=%d", nodeID, len(bundle.Users), len(bundle.Tunnels))
	return nil
}

func prepareAgentData(dataDir, npsWorkDir, token string, npsPort, npsTLSPort, npsHTTPPort, npsHTTPSPort int) error {
	if err := os.MkdirAll(filepath.Join(npsWorkDir, "conf"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(npsWorkDir, "data"), 0o755); err != nil {
		return err
	}
	if err := copyDefaultNPSConf(filepath.Join(npsWorkDir, "conf")); err != nil {
		return err
	}
	confPath := filepath.Join(npsWorkDir, "conf", "nps.conf")
	conf, err := defaultNPSConf(npsWorkDir)
	if err != nil {
		return err
	}
	replacements := map[string]string{
		"http_proxy_port":  strconv.Itoa(npsHTTPPort),
		"https_proxy_port": strconv.Itoa(npsHTTPSPort),
		"bridge_port":      strconv.Itoa(npsPort),
		"tls_bridge_port":  strconv.Itoa(npsTLSPort),
		"log_path":         filepath.Join(npsWorkDir, "data", "nps.log"),
		"web_port":         "0",
		"web_ip":           "127.0.0.1",
		"auth_key":         token,
		"allow_ports":      "1-65535",
	}
	conf = replaceNPSConfig(conf, replacements)
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		return err
	}
	_ = dataDir
	return nil
}

func copyDefaultNPSConf(dst string) error {
	src := "/opt/tunnel-control/defaults/nps/conf"
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		target := filepath.Join(dst, entry.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func defaultNPSConf(workDir string) (string, error) {
	if b, err := os.ReadFile("/opt/tunnel-control/defaults/nps/conf/nps.conf"); err == nil {
		return string(b), nil
	}
	return fmt.Sprintf("appname = nps\nrunmode = pro\nhttp_proxy_ip=0.0.0.0\nhttp_proxy_port=9080\nhttps_proxy_port=9443\nbridge_type=tcp\nbridge_port=18024\nbridge_ip=0.0.0.0\ntls_enable=true\ntls_bridge_port=18025\nlog_level=6\nlog_path=%s\nweb_port=0\nweb_ip=127.0.0.1\nauth_key=agent\nauth_crypt_key=change16bytekey\nallow_ports=1-65535\n", filepath.Join(workDir, "data", "nps.log")), nil
}

func replaceNPSConfig(conf string, replacements map[string]string) string {
	lines := strings.Split(conf, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		value, ok := replacements[key]
		if !ok {
			continue
		}
		lines[i] = key + "=" + value
		seen[key] = true
	}
	for key, value := range replacements {
		if !seen[key] {
			lines = append(lines, key+"="+value)
		}
	}
	return strings.Join(lines, "\n")
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvAllowEmpty(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if ok {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(os.Getenv(key), "%d", &value); err == nil && value > 0 {
		return value
	}
	return fallback
}

func reportTrafficOnce(controlURL, nodeID, nodeToken string) error {
	tunnels, users := integrated.CollectNPSTraffic()
	if len(tunnels) == 0 && len(users) == 0 {
		return nil
	}
	report := core.TrafficReport{
		NodeID:  nodeID,
		Tunnels: tunnels,
		Users:   users,
	}
	b, err := json.Marshal(report)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, controlURL+"/api/agent/report", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+nodeToken)

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("central returned status %s", resp.Status)
	}
	return nil
}
