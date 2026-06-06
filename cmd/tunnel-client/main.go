package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	npsclient "ehang.io/nps/client"
	npsconfig "ehang.io/nps/lib/config"
	"qwernot/tunnel-control/internal/core"
)

type loginResponse struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type bootstrapResponse struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	User        core.PublicUser `json:"user"`
	Secrets     struct {
		FRPToken     string `json:"frpToken"`
		NPSVerifyKey string `json:"npsVerifyKey"`
	} `json:"secrets"`
	Nodes []bootstrapNode `json:"nodes"`
}

type bootstrapNode struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Runtime core.RuntimeConfig `json:"runtime"`
	Tunnels []core.Tunnel      `json:"tunnels"`
}

func main() {
	controlURL := flag.String("server", getenv("CONTROL_URL", "http://127.0.0.1:8088"), "central control URL")
	username := flag.String("user", getenv("TUNNEL_USER", ""), "control panel username")
	password := flag.String("password", getenv("TUNNEL_PASSWORD", ""), "control panel password")
	refresh := flag.Duration("refresh", 30*time.Second, "bootstrap refresh interval")
	flag.Parse()

	if *username == "" || *password == "" {
		log.Fatal("TUNNEL_USER/TUNNEL_PASSWORD or -user/-password is required")
	}
	client, err := newControlClient()
	if err != nil {
		log.Fatal(err)
	}
	if err := loginControl(client, *controlURL, *username, *password); err != nil {
		log.Fatal(err)
	}
	log.Printf("logged in to %s as %s", strings.TrimRight(*controlURL, "/"), *username)

	startedNPS := map[string]bool{}
	if err := syncAndStart(client, *controlURL, startedNPS); err != nil {
		log.Printf("initial sync failed: %v", err)
	}
	ticker := time.NewTicker(*refresh)
	defer ticker.Stop()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-ticker.C:
			if err := syncAndStart(client, *controlURL, startedNPS); err != nil {
				log.Printf("sync failed: %v", err)
			}
		case <-stop:
			log.Printf("stopping tunnel-client")
			return
		}
	}
}

func newControlClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{Jar: jar, Timeout: 12 * time.Second}, nil
}

func loginControl(client *http.Client, controlURL, username, password string) error {
	body, _ := json.Marshal(map[string]string{"name": username, "password": password})
	req, err := http.NewRequest(http.MethodPost, endpoint(controlURL, "/api/login"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: %s", resp.Status)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Name == "" {
		return fmt.Errorf("login returned empty user")
	}
	return nil
}

func syncAndStart(client *http.Client, controlURL string, startedNPS map[string]bool) error {
	bundle, err := fetchBootstrap(client, controlURL)
	if err != nil {
		return err
	}
	for _, node := range bundle.Nodes {
		if countNPSTunnels(node.Tunnels) > 0 {
			if bundle.Secrets.NPSVerifyKey == "" {
				log.Printf("user %s has NPS tunnels but no NPS key; node=%s skipped", bundle.User.Name, node.ID)
			} else if !startedNPS[node.ID] {
				startedNPS[node.ID] = true
				startNPSClient(node, bundle.Secrets.NPSVerifyKey)
			}
		}
	}
	if len(startedNPS) == 0 {
		log.Printf("no enabled tunnels for user %s", bundle.User.Name)
	}
	return nil
}

func startNPSClient(node bootstrapNode, verifyKey string) {
	server := npsServerAddr(node.Runtime)
	log.Printf("starting NPS client for node=%s server=%s tunnels=%d", node.ID, server, countNPSTunnels(node.Tunnels))
	go func(nodeID, server string) {
		cnf := &npsconfig.Config{
			CommonConfig: &npsconfig.CommonConfig{
				Server:         server,
				VKey:           verifyKey,
				Tp:             "tcp",
				DisconnectTime: 60,
			},
		}
		npsclient.NewRPClient(server, verifyKey, "tcp", "", cnf, 60).Start()
		log.Printf("NPS client exited for node=%s server=%s", nodeID, server)
	}(node.ID, server)
}

func fetchBootstrap(client *http.Client, controlURL string) (bootstrapResponse, error) {
	resp, err := client.Get(endpoint(controlURL, "/api/client/bootstrap"))
	if err != nil {
		return bootstrapResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return bootstrapResponse{}, fmt.Errorf("bootstrap failed: %s", resp.Status)
	}
	var out bootstrapResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return bootstrapResponse{}, err
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	return out, nil
}

func countNPSTunnels(tunnels []core.Tunnel) int {
	count := 0
	for _, tunnel := range tunnels {
		if tunnel.Enabled && tunnel.Engine == core.EngineNPS {
			count++
		}
	}
	return count
}

func npsServerAddr(runtime core.RuntimeConfig) string {
	runtime = runtime.WithDefaultsForClient()
	return net.JoinHostPort(runtime.ServerAddr, fmt.Sprintf("%d", runtime.NPSServerPort))
}

func endpoint(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func getenv(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
