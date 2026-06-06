package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
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

//go:embed web/*
var embeddedWeb embed.FS

type contextKey string

const userContextKey contextKey = "user"

func main() {
	logs := newLogBuffer(3000)
	setupLogging(logs)
	var (
		addr             = flag.String("addr", ":8088", "HTTP listen address")
		dbPath           = flag.String("db", ".data/tunnel-control.json", "JSON database path")
		publicAddr       = flag.String("public-addr", "127.0.0.1", "public server address used in generated client configs")
		frpServerPort    = flag.Int("frp-port", 7000, "frps bind port used in generated frpc configs")
		frpHTTPPort      = flag.Int("frp-http-port", 0, "FRP HTTP vhost port used in generated local-node configs")
		frpHTTPSPort     = flag.Int("frp-https-port", 0, "FRP HTTPS vhost port used in generated local-node configs")
		npsServerPort    = flag.Int("nps-port", 8024, "nps bridge port used in generated npc commands")
		npsHTTPPort      = flag.Int("nps-http-port", 0, "NPS HTTP proxy port shown in runtime info")
		npsHTTPSPort     = flag.Int("nps-https-port", 0, "NPS HTTPS proxy port shown in runtime info")
		frpsBin          = flag.String("frps-bin", "", "accepted for compatibility; ignored by control-only mode")
		frpsConfig       = flag.String("frps-config", "", "accepted for compatibility; ignored by control-only mode")
		npsBin           = flag.String("nps-bin", "", "accepted for compatibility; ignored by control-only mode")
		npsWorkDir       = flag.String("nps-workdir", "", "accepted for compatibility; ignored by control-only mode")
		frpDashboardPort = flag.Int("frp-dashboard-port", 0, "accepted for compatibility; ignored by control-only mode")
		frpUsersPath     = flag.String("frp-users-path", ".data/frps-users.json", "path written by FRP userStore sync")
		npsClientsPath   = flag.String("nps-clients-path", "", "optional path written by NPS client sync")
		configOutDir     = flag.String("config-out-dir", ".data/export", "directory written by full config export")
		adminUser        = flag.String("admin-user", "admin", "bootstrap admin user if no enabled admin exists")
		adminPassword    = flag.String("admin-password", "admin123", "bootstrap admin password if no enabled admin exists")
		agentPushPort    = flag.Int("agent-push-port", 18089, "remote tunnel-agent push API port")
	)
	flag.Parse()
	_ = frpsBin
	_ = frpsConfig
	_ = npsBin
	_ = npsWorkDir
	_ = frpDashboardPort

	store, err := core.NewStore(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	createdAdmin, err := store.EnsureAdmin(*adminUser, *adminPassword)
	if err != nil {
		log.Fatal(err)
	}
	if createdAdmin {
		log.Printf("created bootstrap admin user %q", *adminUser)
	}
	api := &apiServer{
		store:         store,
		sessions:      newSessionStore(),
		remoteClients: map[string][]integrated.ClientStatus{},
		frpUserOut:    *frpUsersPath,
		npsClientOut:  *npsClientsPath,
		configOut:     *configOutDir,
		embedded:      false,
		listenAddr:    *addr,
		logs:          logs,
		agentPushPort: *agentPushPort,
		runtime: core.RuntimeConfig{
			ServerAddr:       *publicAddr,
			FRPServerPort:    *frpServerPort,
			FRPHTTPPort:      *frpHTTPPort,
			FRPHTTPSPort:     *frpHTTPSPort,
			NPSServerPort:    *npsServerPort,
			NPSHTTPProxyPort: *npsHTTPPort,
			NPSHTTPSPort:     *npsHTTPSPort,
		},
	}
	if _, err := api.syncEngineUsers(); err != nil {
		log.Printf("initial engine user sync failed: %v", err)
	}
	go api.pushRemoteNodesLoop(context.Background())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/login", api.login)
	mux.HandleFunc("POST /api/logout", api.logout)
	mux.HandleFunc("GET /api/me", api.me)
	mux.HandleFunc("POST /api/password", api.changePassword)
	mux.HandleFunc("GET /api/users", api.listUsers)
	mux.HandleFunc("POST /api/users", api.upsertUser)
	mux.HandleFunc("DELETE /api/users/", api.deleteUser)
	mux.HandleFunc("GET /api/agent/bundle", api.agentBundle)
	mux.HandleFunc("GET /api/nodes", api.listNodes)
	mux.HandleFunc("POST /api/nodes", api.upsertNode)
	mux.HandleFunc("DELETE /api/nodes/", api.deleteNode)
	mux.HandleFunc("GET /api/tunnels", api.listTunnels)
	mux.HandleFunc("POST /api/tunnels", api.createTunnel)
	mux.HandleFunc("PUT /api/tunnels/", api.updateTunnel)
	mux.HandleFunc("DELETE /api/tunnels/", api.deleteTunnel)
	mux.HandleFunc("GET /api/diagnostics", api.diagnostics)
	mux.HandleFunc("GET /api/clients", api.listClients)
	mux.HandleFunc("GET /api/client/bootstrap", api.clientBootstrap)
	mux.HandleFunc("GET /api/availability", api.listAvailability)
	mux.HandleFunc("GET /api/runtime", api.runtimeInfo)
	mux.HandleFunc("GET /api/logs", api.listLogs)
	mux.HandleFunc("DELETE /api/logs", api.clearLogs)
	mux.HandleFunc("POST /api/sync/frp-users", api.syncFRPUsers)
	mux.HandleFunc("POST /api/export/configs", api.exportConfigs)
	mux.HandleFunc("GET /api/users/", api.userConfig)
	mux.HandleFunc("GET /api/export/frp-users.json", api.exportFRPUsers)
	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	log.Printf("tunnel-control listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, logRequest(api.authMiddleware(mux))))
}

type apiServer struct {
	store         *core.Store
	sessions      *sessionStore
	frpUserOut    string
	npsClientOut  string
	configOut     string
	embedded      bool
	listenAddr    string
	logs          *logBuffer
	agentPushPort int
	remoteMu      sync.RWMutex
	remoteClients map[string][]integrated.ClientStatus
	runtime       core.RuntimeConfig
}

type agentSyncBundle struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Node        core.Node     `json:"node"`
	Users       []core.User   `json:"users"`
	Tunnels     []core.Tunnel `json:"tunnels"`
}

type clientBootstrapResponse struct {
	GeneratedAt time.Time             `json:"generatedAt"`
	User        core.PublicUser       `json:"user"`
	Secrets     clientBootstrapSecret `json:"secrets"`
	Nodes       []clientBootstrapNode `json:"nodes"`
}

type clientBootstrapSecret struct {
	FRPToken     string `json:"frpToken,omitempty"`
	NPSVerifyKey string `json:"npsVerifyKey,omitempty"`
}

type clientBootstrapNode struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Runtime core.RuntimeConfig `json:"runtime"`
	Tunnels []core.Tunnel      `json:"tunnels"`
}

type resourceUsage struct {
	UserName     string `json:"userName"`
	PortTotal    int    `json:"portTotal"`
	TCPUsed      int    `json:"tcpUsed"`
	TCPFree      int    `json:"tcpFree"`
	UDPUsed      int    `json:"udpUsed"`
	UDPFree      int    `json:"udpFree"`
	DomainTotal  int    `json:"domainTotal"`
	DomainUsed   int    `json:"domainUsed"`
	DomainFree   int    `json:"domainFree"`
	TunnelUsed   int    `json:"tunnelUsed"`
	TunnelLimit  int    `json:"tunnelLimit"`
	TunnelFree   int    `json:"tunnelFree"`
	HasWildcard  bool   `json:"hasWildcard"`
	LimitMessage string `json:"limitMessage,omitempty"`
}

type clientRuntimeStatus struct {
	NodeID         string `json:"nodeId,omitempty"`
	UserName       string `json:"userName"`
	Engine         string `json:"engine"`
	State          string `json:"state"`
	Online         bool   `json:"online"`
	ClientID       string `json:"clientId,omitempty"`
	ClientIP       string `json:"clientIp,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	Version        string `json:"version,omitempty"`
	ConnectedAt    string `json:"connectedAt,omitempty"`
	LastSeenAt     string `json:"lastSeenAt,omitempty"`
	DisconnectedAt string `json:"disconnectedAt,omitempty"`
	CurrentConns   int    `json:"currentConns,omitempty"`
	TunnelTotal    int    `json:"tunnelTotal"`
	TunnelOnline   int    `json:"tunnelOnline"`
}

type tunnelAvailability struct {
	TunnelID    string                  `json:"tunnelId"`
	UserName    string                  `json:"userName"`
	Engine      string                  `json:"engine"`
	Mode        string                  `json:"mode"`
	State       string                  `json:"state"`
	Message     string                  `json:"message"`
	ClientState string                  `json:"clientState"`
	Entry       tunnelAvailabilityProbe `json:"entry"`
	CheckedAt   string                  `json:"checkedAt"`
}

type tunnelAvailabilityProbe struct {
	State      string `json:"state"`
	Target     string `json:"target"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode,omitempty"`
	LatencyMS  int64  `json:"latencyMs,omitempty"`
}

type clientTunnelCount struct {
	total  int
	online int
}

type sessionStore struct {
	mu     sync.RWMutex
	values map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{values: map[string]string{}}
}

func (s *sessionStore) create(user string) string {
	token := randomToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[token] = user
	return token
}

func (s *sessionStore) get(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.values[token]
	return user, ok
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, token)
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func (a *apiServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/login" || strings.HasPrefix(r.URL.Path, "/api/agent/") {
			next.ServeHTTP(w, r)
			return
		}
		user, ok := a.currentUser(r)
		if !ok {
			writeErrorText(w, http.StatusUnauthorized, "login required")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *apiServer) currentUser(r *http.Request) (*core.User, bool) {
	cookie, err := r.Cookie("tc_session")
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	name, ok := a.sessions.get(cookie.Value)
	if !ok {
		return nil, false
	}
	user, ok := a.store.GetUser(name)
	return user, ok && user.Enabled
}

func requestUser(r *http.Request) *core.User {
	user, _ := r.Context().Value(userContextKey).(*core.User)
	return user
}

func isAdmin(user *core.User) bool {
	return user != nil && user.Role == core.RoleAdmin
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if isAdmin(requestUser(r)) {
		return true
	}
	writeErrorText(w, http.StatusForbidden, "admin required")
	return false
}

func (a *apiServer) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, ok := a.store.VerifyLogin(req.Name, req.Password)
	if !ok {
		writeErrorText(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token := a.sessions.create(user.Name)
	http.SetCookie(w, &http.Cookie{
		Name:     "tc_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, core.Public(user))
}

func (a *apiServer) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("tc_session"); err == nil {
		a.sessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "tc_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (a *apiServer) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, core.Public(requestUser(r)))
}

func (a *apiServer) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.NewPassword == "" {
		writeErrorText(w, http.StatusBadRequest, "new password is required")
		return
	}
	current := requestUser(r)
	target := req.Name
	if target == "" {
		target = current.Name
	}
	if !isAdmin(current) {
		if target != current.Name {
			writeErrorText(w, http.StatusForbidden, "cannot change another user's password")
			return
		}
		if _, ok := a.store.VerifyLogin(current.Name, req.OldPassword); !ok {
			writeErrorText(w, http.StatusBadRequest, "old password is incorrect")
			return
		}
	}
	if err := a.store.SetPassword(target, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password changed"})
}

func (a *apiServer) listUsers(w http.ResponseWriter, r *http.Request) {
	user := requestUser(r)
	if isAdmin(user) {
		writeJSON(w, http.StatusOK, a.store.ListUsers())
		return
	}
	writeJSON(w, http.StatusOK, []core.PublicUser{core.Public(user)})
}

func (a *apiServer) upsertUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var req struct {
		Name         string           `json:"name"`
		Password     string           `json:"password"`
		Role         string           `json:"role"`
		Enabled      *bool            `json:"enabled"`
		PortPool     string           `json:"portPool"`
		PortPools    []core.PortRange `json:"portPools"`
		DomainPool   string           `json:"domainPool"`
		DomainPools  []string         `json:"domainPools"`
		MaxPorts     int              `json:"maxPorts"`
		FRPToken     string           `json:"frpToken"`
		NPSVerifyKey string           `json:"npsVerifyKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	pools := req.PortPools
	if req.PortPool != "" {
		parsed, err := core.ParsePortRanges(req.PortPool)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		pools = parsed
	}
	domainPools := req.DomainPools
	if req.DomainPool != "" {
		parsed, err := core.ParseDomainPools(req.DomainPool)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		domainPools = parsed
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	user, err := a.store.UpsertUser(core.User{
		Name:         req.Name,
		Password:     req.Password,
		Role:         req.Role,
		Enabled:      enabled,
		PortPools:    pools,
		DomainPools:  domainPools,
		MaxPorts:     req.MaxPorts,
		FRPToken:     req.FRPToken,
		NPSVerifyKey: req.NPSVerifyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := a.syncEngineUsers(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, user)
}

func (a *apiServer) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	if err := a.store.DeleteUser(name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := a.syncEngineUsers(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *apiServer) listNodes(w http.ResponseWriter, r *http.Request) {
	if isAdmin(requestUser(r)) {
		writeJSON(w, http.StatusOK, a.store.ListNodes())
		return
	}
	nodes := make([]core.Node, 0)
	for _, node := range a.store.ListNodes() {
		if node.ID == core.DefaultNodeID || !node.Enabled {
			continue
		}
		node.Token = ""
		nodes = append(nodes, node)
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (a *apiServer) upsertNode(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var req struct {
		ID          string             `json:"id"`
		Name        string             `json:"name"`
		Token       string             `json:"token"`
		PublicAddr  string             `json:"publicAddr"`
		Enabled     *bool              `json:"enabled"`
		FRPEnabled  *bool              `json:"frpEnabled"`
		NPSEnabled  *bool              `json:"npsEnabled"`
		PortPool    string             `json:"portPool"`
		PortPools   []core.PortRange   `json:"portPools"`
		DomainPool  string             `json:"domainPool"`
		DomainPools []string           `json:"domainPools"`
		Runtime     core.RuntimeConfig `json:"runtime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	pools := req.PortPools
	if req.PortPool != "" {
		parsed, err := core.ParsePortRanges(req.PortPool)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		pools = parsed
	}
	domainPools := req.DomainPools
	if req.DomainPool != "" {
		parsed, err := core.ParseDomainPools(req.DomainPool)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		domainPools = parsed
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	npsEnabled := true
	if req.NPSEnabled != nil {
		npsEnabled = *req.NPSEnabled
	}
	node, err := a.store.UpsertNode(core.Node{
		ID:          req.ID,
		Name:        req.Name,
		Token:       req.Token,
		PublicAddr:  req.PublicAddr,
		Enabled:     enabled,
		FRPEnabled:  false,
		NPSEnabled:  npsEnabled,
		PortPools:   pools,
		DomainPools: domainPools,
		Runtime:     req.Runtime,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, node)
}

func (a *apiServer) deleteNode(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if err := a.store.DeleteNode(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *apiServer) agentBundle(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("node"))
	if nodeID == "" {
		writeErrorText(w, http.StatusBadRequest, "node is required")
		return
	}
	node, ok := a.store.GetNode(nodeID)
	if !ok || !node.Enabled {
		writeErrorText(w, http.StatusNotFound, "node not found or disabled")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || node.Token == "" || token != node.Token {
		writeErrorText(w, http.StatusUnauthorized, "invalid node token")
		return
	}
	writeJSON(w, http.StatusOK, a.bundleForNode(node))
}

func (a *apiServer) bundleForNode(node core.Node) agentSyncBundle {
	tunnels := filterTunnelsByNode(a.store.ListTunnels(""), node.ID)
	users := usersForTunnels(a.store.Users(), tunnels)
	return agentSyncBundle{
		GeneratedAt: time.Now().UTC(),
		Node:        node,
		Users:       users,
		Tunnels:     tunnels,
	}
}

func (a *apiServer) pushRemoteNodesAsync() {
	go a.pushRemoteNodes(context.Background())
}

func (a *apiServer) pushRemoteNodesLoop(ctx context.Context) {
	time.Sleep(5 * time.Second)
	a.pushRemoteNodes(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pushRemoteNodes(ctx)
		}
	}
}

func (a *apiServer) pushRemoteNodes(ctx context.Context) {
	for _, node := range a.store.ListNodes() {
		if node.ID == core.DefaultNodeID || !node.Enabled || node.Token == "" || strings.TrimSpace(node.PublicAddr) == "" {
			continue
		}
		if err := a.pushRemoteNode(ctx, node); err != nil {
			log.Printf("push node %s failed: %v", node.ID, err)
			a.setRemoteClientStatuses(node.ID, nil)
			if statusErr := a.store.UpdateNodeStatus(node.ID, false, err.Error()); statusErr != nil {
				log.Printf("update node %s status failed: %v", node.ID, statusErr)
			}
			continue
		}
		if err := a.store.UpdateNodeStatus(node.ID, true, ""); err != nil {
			log.Printf("update node %s status failed: %v", node.ID, err)
		}
	}
}

func (a *apiServer) pushRemoteNode(ctx context.Context, node core.Node) error {
	bundle := a.bundleForNode(node)
	body, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, agentApplyURL(node.PublicAddr, a.agentPushPort), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+node.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("agent returned %s", resp.Status)
	}
	var applyResp struct {
		Clients []integrated.ClientStatus `json:"clients"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applyResp); err != nil && err != io.EOF {
		return err
	}
	a.setRemoteClientStatuses(node.ID, applyResp.Clients)
	log.Printf("pushed node=%s users=%d tunnels=%d", node.ID, len(bundle.Users), len(bundle.Tunnels))
	return nil
}

func (a *apiServer) setRemoteClientStatuses(nodeID string, statuses []integrated.ClientStatus) {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if len(statuses) == 0 {
		delete(a.remoteClients, nodeID)
		return
	}
	cp := append([]integrated.ClientStatus(nil), statuses...)
	a.remoteClients[nodeID] = cp
}

func (a *apiServer) allClientStatuses() []integrated.ClientStatus {
	statuses := runtimeClientStatuses()
	a.remoteMu.RLock()
	defer a.remoteMu.RUnlock()
	for _, nodeStatuses := range a.remoteClients {
		statuses = append(statuses, nodeStatuses...)
	}
	return statuses
}

func agentApplyURL(publicAddr string, port int) string {
	addr := strings.TrimRight(strings.TrimSpace(publicAddr), "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr + "/api/agent/apply"
	}
	if strings.Contains(addr, ":") && !strings.Contains(addr, "]") {
		return "http://" + addr + "/api/agent/apply"
	}
	return "http://" + net.JoinHostPort(addr, strconv.Itoa(port)) + "/api/agent/apply"
}

func (a *apiServer) listTunnels(w http.ResponseWriter, r *http.Request) {
	user := requestUser(r)
	if isAdmin(user) {
		writeJSON(w, http.StatusOK, a.store.ListTunnels(r.URL.Query().Get("user")))
		return
	}
	writeJSON(w, http.StatusOK, a.store.ListTunnels(user.Name))
}

func (a *apiServer) createTunnel(w http.ResponseWriter, r *http.Request) {
	var req core.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user := requestUser(r)
	if !isAdmin(user) {
		req.UserName = user.Name
	}
	tunnel, err := a.store.CreateTunnel(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.syncEmbeddedNPS(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, tunnel)
}

func (a *apiServer) updateTunnel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tunnels/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var req core.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user := requestUser(r)
	if !isAdmin(user) {
		existing := a.store.ListTunnels(user.Name)
		allowed := false
		for _, t := range existing {
			if t.ID == id {
				allowed = true
				break
			}
		}
		if !allowed {
			writeErrorText(w, http.StatusForbidden, "cannot update another user's tunnel")
			return
		}
		req.UserName = user.Name
	}
	tunnel, err := a.store.UpdateTunnel(id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.syncEmbeddedNPS(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, tunnel)
}

func (a *apiServer) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tunnels/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	user := requestUser(r)
	if !isAdmin(user) {
		allowed := false
		for _, t := range a.store.ListTunnels(user.Name) {
			if t.ID == id {
				allowed = true
				break
			}
		}
		if !allowed {
			writeErrorText(w, http.StatusForbidden, "cannot delete another user's tunnel")
			return
		}
	}
	if err := a.store.DeleteTunnel(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.syncEmbeddedNPS(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.pushRemoteNodesAsync()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *apiServer) runtimeInfo(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"client":         a.runtime,
		"frpUsersPath":   a.frpUserOut,
		"npsClientsPath": a.npsClientOut,
		"configOutDir":   a.configOut,
	}
	if isAdmin(requestUser(r)) {
		resp["nodes"] = a.store.ListNodes()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *apiServer) diagnostics(w http.ResponseWriter, r *http.Request) {
	current := requestUser(r)
	users := a.store.Users()
	tunnels := a.store.ListTunnels("")
	if !isAdmin(current) {
		filteredUsers := make([]core.User, 0, 1)
		for _, user := range users {
			if user.Name == current.Name {
				filteredUsers = append(filteredUsers, user)
				break
			}
		}
		users = filteredUsers
		filteredTunnels := make([]core.Tunnel, 0)
		for _, tunnel := range tunnels {
			if tunnel.UserName == current.Name {
				filteredTunnels = append(filteredTunnels, tunnel)
			}
		}
		tunnels = filteredTunnels
	}
	resp := map[string]any{
		"generatedAt": time.Now().UTC(),
		"resources":   resourceUsageFor(users, tunnels),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *apiServer) listClients(w http.ResponseWriter, r *http.Request) {
	current := requestUser(r)
	users := a.store.Users()
	tunnels := a.store.ListTunnels("")
	if !isAdmin(current) {
		users = filterUsersByName(users, current.Name)
		tunnels = filterTunnelsByUser(tunnels, current.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": time.Now().UTC(),
		"clients":     clientStatusesFor(users, tunnels, a.embedded, a.allClientStatuses()),
	})
}

func (a *apiServer) clientBootstrap(w http.ResponseWriter, r *http.Request) {
	current := requestUser(r)
	userName := current.Name
	if isAdmin(current) && r.URL.Query().Get("user") != "" {
		userName = r.URL.Query().Get("user")
	}
	user, ok := a.store.GetUser(userName)
	if !ok || !user.Enabled {
		writeErrorText(w, http.StatusNotFound, "user not found")
		return
	}
	tunnels := a.store.ListTunnels(user.Name)
	nodeByID := map[string]core.Node{}
	for _, node := range a.store.ListNodes() {
		nodeByID[node.ID] = node
	}
	grouped := map[string][]core.Tunnel{}
	for _, tunnel := range tunnels {
		if !tunnel.Enabled {
			continue
		}
		nodeID := tunnel.NodeID
		if nodeID == "" {
			nodeID = core.DefaultNodeID
		}
		grouped[nodeID] = append(grouped[nodeID], tunnel)
	}
	nodes := make([]clientBootstrapNode, 0, len(grouped))
	for nodeID, nodeTunnels := range grouped {
		node, ok := nodeByID[nodeID]
		if !ok || !node.Enabled {
			continue
		}
		nodes = append(nodes, clientBootstrapNode{
			ID:      node.ID,
			Name:    node.Name,
			Runtime: runtimeForNode(node, a.runtime),
			Tunnels: nodeTunnels,
		})
	}
	writeJSON(w, http.StatusOK, clientBootstrapResponse{
		GeneratedAt: time.Now().UTC(),
		User:        core.Public(user),
		Secrets: clientBootstrapSecret{
			FRPToken:     user.FRPToken,
			NPSVerifyKey: user.NPSVerifyKey,
		},
		Nodes: nodes,
	})
}

func (a *apiServer) listAvailability(w http.ResponseWriter, r *http.Request) {
	current := requestUser(r)
	users := a.store.Users()
	tunnels := a.store.ListTunnels("")
	if !isAdmin(current) {
		users = filterUsersByName(users, current.Name)
		tunnels = filterTunnelsByUser(tunnels, current.Name)
	}
	liveClients := a.allClientStatuses()
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt":  time.Now().UTC(),
		"availability": availabilityFor(tunnels, liveClients, a.runtimeForTunnel),
	})
}

func filterUsersByName(users []core.User, name string) []core.User {
	out := make([]core.User, 0, 1)
	for _, user := range users {
		if user.Name == name {
			out = append(out, user)
			break
		}
	}
	return out
}

func filterTunnelsByUser(tunnels []core.Tunnel, name string) []core.Tunnel {
	out := make([]core.Tunnel, 0)
	for _, tunnel := range tunnels {
		if tunnel.UserName == name {
			out = append(out, tunnel)
		}
	}
	return out
}

func filterTunnelsByNode(tunnels []core.Tunnel, nodeID string) []core.Tunnel {
	if nodeID == "" {
		nodeID = core.DefaultNodeID
	}
	out := make([]core.Tunnel, 0)
	for _, tunnel := range tunnels {
		current := tunnel.NodeID
		if current == "" {
			current = core.DefaultNodeID
		}
		if current == nodeID {
			out = append(out, tunnel)
		}
	}
	return out
}

func usersForTunnels(users []core.User, tunnels []core.Tunnel) []core.User {
	needed := map[string]bool{}
	for _, tunnel := range tunnels {
		needed[tunnel.UserName] = true
	}
	out := make([]core.User, 0, len(needed))
	for _, user := range users {
		if needed[user.Name] {
			out = append(out, user)
		}
	}
	return out
}

func runtimeForNode(node core.Node, fallback core.RuntimeConfig) core.RuntimeConfig {
	cfg := fallback
	if node.PublicAddr != "" {
		cfg.ServerAddr = node.PublicAddr
	}
	if node.Runtime.ServerAddr != "" {
		cfg.ServerAddr = node.Runtime.ServerAddr
	}
	if node.Runtime.FRPServerPort > 0 {
		cfg.FRPServerPort = node.Runtime.FRPServerPort
	}
	if node.Runtime.FRPHTTPPort > 0 {
		cfg.FRPHTTPPort = node.Runtime.FRPHTTPPort
	}
	if node.Runtime.FRPHTTPSPort > 0 {
		cfg.FRPHTTPSPort = node.Runtime.FRPHTTPSPort
	}
	if node.Runtime.NPSServerPort > 0 {
		cfg.NPSServerPort = node.Runtime.NPSServerPort
	}
	if node.Runtime.NPSHTTPProxyPort > 0 {
		cfg.NPSHTTPProxyPort = node.Runtime.NPSHTTPProxyPort
	}
	if node.Runtime.NPSHTTPSPort > 0 {
		cfg.NPSHTTPSPort = node.Runtime.NPSHTTPSPort
	}
	return cfg
}

func (a *apiServer) runtimeForTunnel(tunnel core.Tunnel) core.RuntimeConfig {
	nodeID := tunnel.NodeID
	if nodeID == "" {
		nodeID = core.DefaultNodeID
	}
	if node, ok := a.store.GetNode(nodeID); ok {
		return runtimeForNode(node, a.runtime)
	}
	return a.runtime
}

func resourceUsageFor(users []core.User, tunnels []core.Tunnel) []resourceUsage {
	out := make([]resourceUsage, 0, len(users))
	for _, user := range users {
		usage := resourceUsage{
			UserName:    user.Name,
			PortTotal:   totalPorts(user.PortPools),
			DomainTotal: len(user.DomainPools),
			TunnelLimit: user.MaxPorts,
		}
		tcpPorts := map[int]bool{}
		udpPorts := map[int]bool{}
		domains := map[string]bool{}
		for _, pool := range user.DomainPools {
			if pool == "*" || strings.HasPrefix(pool, "*.") {
				usage.HasWildcard = true
			}
		}
		for _, tunnel := range tunnels {
			if tunnel.UserName != user.Name {
				continue
			}
			usage.TunnelUsed++
			if !tunnel.Enabled {
				continue
			}
			switch tunnel.Mode {
			case "udp":
				if tunnel.RemotePort > 0 {
					udpPorts[tunnel.RemotePort] = true
				}
			case "tcp", "socks5":
				if tunnel.RemotePort > 0 {
					tcpPorts[tunnel.RemotePort] = true
				}
			case "http", "https":
				for _, domain := range tunnel.Domains {
					domains[domain] = true
				}
			}
		}
		usage.TCPUsed = len(tcpPorts)
		usage.UDPUsed = len(udpPorts)
		usage.DomainUsed = len(domains)
		usage.TCPFree = freeCount(usage.PortTotal, usage.TCPUsed)
		usage.UDPFree = freeCount(usage.PortTotal, usage.UDPUsed)
		if usage.HasWildcard {
			usage.DomainFree = -1
		} else {
			usage.DomainFree = freeCount(usage.DomainTotal, usage.DomainUsed)
		}
		if usage.TunnelLimit > 0 {
			usage.TunnelFree = freeCount(usage.TunnelLimit, usage.TunnelUsed)
		} else {
			usage.TunnelFree = -1
			usage.LimitMessage = "不限"
		}
		out = append(out, usage)
	}
	return out
}

func runtimeClientStatuses() []integrated.ClientStatus {
	statuses := integrated.FRPClientStatuses()
	statuses = append(statuses, integrated.NPSClientStatuses()...)
	for i := range statuses {
		statuses[i].NodeID = core.DefaultNodeID
	}
	return statuses
}

func clientStatusesFor(users []core.User, tunnels []core.Tunnel, embedded bool, live []integrated.ClientStatus) []clientRuntimeStatus {
	liveByUserEngine := map[string]integrated.ClientStatus{}
	for _, status := range live {
		if status.UserName == "" || status.Engine == "" {
			continue
		}
		key := statusKey(status.UserName, status.Engine)
		if current, ok := liveByUserEngine[key]; !ok || betterClientStatus(status, current) {
			liveByUserEngine[key] = status
		}
	}

	tunnelCounts := map[string]clientTunnelCount{}
	for _, tunnel := range tunnels {
		if !tunnel.Enabled {
			continue
		}
		key := statusKey(tunnel.UserName, tunnel.Engine)
		count := tunnelCounts[key]
		count.total++
		tunnelCounts[key] = count
	}

	out := []clientRuntimeStatus{}
	for _, user := range users {
		if user.FRPToken != "" {
			out = append(out, buildClientRuntimeStatus(user.Name, core.EngineFRP, embedded, liveByUserEngine[statusKey(user.Name, core.EngineFRP)], tunnelCounts[statusKey(user.Name, core.EngineFRP)]))
		}
		if user.NPSVerifyKey != "" {
			out = append(out, buildClientRuntimeStatus(user.Name, core.EngineNPS, embedded, liveByUserEngine[statusKey(user.Name, core.EngineNPS)], tunnelCounts[statusKey(user.Name, core.EngineNPS)]))
		}
	}
	return out
}

func buildClientRuntimeStatus(userName, engineName string, embedded bool, live integrated.ClientStatus, count clientTunnelCount) clientRuntimeStatus {
	state := "unknown"
	if embedded {
		state = "offline"
	}
	status := clientRuntimeStatus{
		NodeID:       live.NodeID,
		UserName:     userName,
		Engine:       engineName,
		State:        state,
		TunnelTotal:  count.total,
		TunnelOnline: count.online,
	}
	if live.UserName == "" {
		return status
	}
	status.Online = live.Online
	if live.Online {
		status.State = "online"
		status.TunnelOnline = status.TunnelTotal
	} else if embedded {
		status.State = "offline"
	}
	status.ClientID = live.ClientID
	status.ClientIP = live.ClientIP
	status.Hostname = live.Hostname
	status.Version = live.Version
	status.ConnectedAt = statusTime(live.ConnectedAt)
	status.LastSeenAt = statusTime(live.LastSeenAt)
	status.DisconnectedAt = statusTime(live.DisconnectedAt)
	status.CurrentConns = live.CurrentConns
	return status
}

func betterClientStatus(candidate, current integrated.ClientStatus) bool {
	if candidate.Online != current.Online {
		return candidate.Online
	}
	return candidate.LastSeenAt.After(current.LastSeenAt)
}

func statusKey(userName, engineName string) string {
	return userName + "\x00" + engineName
}

func statusTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func availabilityFor(tunnels []core.Tunnel, live []integrated.ClientStatus, runtimeFor func(core.Tunnel) core.RuntimeConfig) []tunnelAvailability {
	out := make([]tunnelAvailability, len(tunnels))
	var wg sync.WaitGroup
	for i, tunnel := range tunnels {
		i, tunnel := i, tunnel
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := clientStatusForTunnel(tunnel, live)
			out[i] = checkTunnelAvailability(tunnel, client, runtimeFor(tunnel))
		}()
	}
	wg.Wait()
	return out
}

func clientStatusForTunnel(tunnel core.Tunnel, live []integrated.ClientStatus) clientRuntimeStatus {
	nodeID := tunnel.NodeID
	if nodeID == "" {
		nodeID = core.DefaultNodeID
	}
	var best integrated.ClientStatus
	for _, status := range live {
		if status.UserName != tunnel.UserName || status.Engine != tunnel.Engine {
			continue
		}
		if status.NodeID != "" && status.NodeID != nodeID {
			continue
		}
		if best.UserName == "" || betterClientStatus(status, best) {
			best = status
		}
	}
	count := clientTunnelCount{total: 1}
	return buildClientRuntimeStatus(tunnel.UserName, tunnel.Engine, nodeID == core.DefaultNodeID, best, count)
}

func checkTunnelAvailability(tunnel core.Tunnel, client clientRuntimeStatus, runtime core.RuntimeConfig) tunnelAvailability {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	clientState := client.State
	if clientState == "" {
		clientState = "unknown"
	}
	result := tunnelAvailability{
		TunnelID:    tunnel.ID,
		UserName:    tunnel.UserName,
		Engine:      tunnel.Engine,
		Mode:        tunnel.Mode,
		ClientState: clientState,
		CheckedAt:   checkedAt,
	}
	if !tunnel.Enabled {
		result.State = "disabled"
		result.Message = "隧道已停用"
		result.Entry = tunnelAvailabilityProbe{State: "disabled", Message: "未检测"}
		return result
	}
	result.Entry = probeTunnelEntry(tunnel, runtime)
	result.State, result.Message = summarizeTunnelAvailability(clientState, result.Entry)
	return result
}

func summarizeTunnelAvailability(clientState string, entry tunnelAvailabilityProbe) (string, string) {
	if entry.State == "down" {
		return "down", "服务端入口异常"
	}
	if clientState == "offline" {
		return "down", "客户端离线"
	}
	if entry.State == "warning" {
		return "warning", entry.Message
	}
	if clientState == "online" && entry.State == "ok" {
		return "ok", "客户端在线，入口可访问"
	}
	if clientState == "online" && entry.State == "unknown" {
		return "warning", entry.Message
	}
	if clientState == "unknown" && entry.State == "ok" {
		return "warning", "入口可访问，客户端状态未知"
	}
	if clientState == "unknown" {
		return "unknown", "客户端状态未知"
	}
	return "warning", "可用性需要关注"
}

func probeTunnelEntry(tunnel core.Tunnel, runtime core.RuntimeConfig) tunnelAvailabilityProbe {
	host := probeHost(runtime.ServerAddr)
	switch tunnel.Mode {
	case "tcp", "socks5":
		return probeTCPEntry(host, tunnel.RemotePort)
	case "udp":
		return tunnelAvailabilityProbe{
			State:   "unknown",
			Target:  net.JoinHostPort(host, strconv.Itoa(tunnel.RemotePort)) + "/udp",
			Message: "UDP 入口无法可靠主动探测，请结合客户端在线和业务流量确认",
		}
	case "http", "https":
		return probeHTTPEntry(tunnel, runtime)
	default:
		return tunnelAvailabilityProbe{State: "unknown", Message: "未知隧道类型"}
	}
}

func probeTCPEntry(host string, port int) tunnelAvailabilityProbe {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	if port <= 0 {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: "远程端口未配置"}
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 1200*time.Millisecond)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: err.Error(), LatencyMS: latency}
	}
	_ = conn.Close()
	return tunnelAvailabilityProbe{State: "ok", Target: target, Message: "TCP 入口已监听", LatencyMS: latency}
}

func probeHTTPEntry(tunnel core.Tunnel, runtime core.RuntimeConfig) tunnelAvailabilityProbe {
	domain := ""
	if len(tunnel.Domains) > 0 {
		domain = tunnel.Domains[0]
	}
	port := httpEntryPort(tunnel.Engine, tunnel.Mode, runtime)
	target := tunnel.Mode + "://" + domain
	if port > 0 {
		target += ":" + strconv.Itoa(port)
	}
	if domain == "" {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: "域名未配置"}
	}
	if port <= 0 {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: "HTTP/HTTPS 入口端口未配置"}
	}
	scheme := tunnel.Mode
	url := scheme + "://" + net.JoinHostPort(probeHost(runtime.ServerAddr), strconv.Itoa(port)) + "/"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: err.Error()}
	}
	req.Host = domain
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 1200 * time.Millisecond,
		}).DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Timeout: 1800 * time.Millisecond, Transport: transport}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return tunnelAvailabilityProbe{State: "down", Target: target, Message: err.Error(), LatencyMS: latency}
	}
	defer resp.Body.Close()
	state := "ok"
	message := "HTTP 入口已响应"
	if resp.StatusCode >= 500 {
		state = "warning"
		message = "入口已响应，但上游返回 " + strconv.Itoa(resp.StatusCode)
	}
	return tunnelAvailabilityProbe{State: state, Target: target, Message: message, StatusCode: resp.StatusCode, LatencyMS: latency}
}

func probeHost(serverAddr string) string {
	addr := strings.TrimSpace(serverAddr)
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	if slash := strings.Index(addr, "/"); slash >= 0 {
		addr = addr[:slash]
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	if addr == "" {
		return "127.0.0.1"
	}
	return addr
}

func httpEntryPort(engineName, mode string, runtime core.RuntimeConfig) int {
	switch {
	case engineName == core.EngineFRP && mode == "http":
		return runtime.FRPHTTPPort
	case engineName == core.EngineFRP && mode == "https":
		return runtime.FRPHTTPSPort
	case engineName == core.EngineNPS && mode == "http":
		return runtime.NPSHTTPProxyPort
	case engineName == core.EngineNPS && mode == "https":
		return runtime.NPSHTTPSPort
	default:
		return 0
	}
}

func totalPorts(ranges []core.PortRange) int {
	total := 0
	for _, r := range ranges {
		if r.End >= r.Start {
			total += r.End - r.Start + 1
		}
	}
	return total
}

func freeCount(total, used int) int {
	if total <= 0 {
		return 0
	}
	if used >= total {
		return 0
	}
	return total - used
}

func (a *apiServer) listLogs(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	limit := 300
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeErrorText(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	query := r.URL.Query().Get("q")
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": time.Now().UTC(),
		"entries":     a.logs.list(limit, query),
	})
}

func (a *apiServer) clearLogs(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	a.logs.clear()
	log.Printf("logs cleared by %s", requestUser(r).Name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (a *apiServer) syncFRPUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	paths, err := a.syncEngineUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "synced", "paths": paths})
}

func (a *apiServer) syncEngineUsers() (map[string]string, error) {
	users := a.store.Users()
	paths := map[string]string{}
	frpUsers, err := core.ExportFRPUsers(users)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(a.frpUserOut), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(a.frpUserOut, append(frpUsers, '\n'), 0o600); err != nil {
		return nil, err
	}
	paths["frp"] = a.frpUserOut
	if a.npsClientOut != "" {
		npsClients, err := core.ExportNPSClients(users)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(a.npsClientOut), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(a.npsClientOut, npsClients, 0o600); err != nil {
			return nil, err
		}
		paths["nps"] = a.npsClientOut
	}
	if err := a.syncEmbeddedAll(); err != nil {
		return nil, err
	}
	return paths, nil
}

func (a *apiServer) syncEmbeddedAll() error {
	if err := a.syncEmbeddedFRP(); err != nil {
		return err
	}
	return a.syncEmbeddedNPS()
}

func (a *apiServer) syncEmbeddedFRP() error {
	if !a.embedded {
		return nil
	}
	return integrated.SyncFRPState(a.store.Users())
}

func (a *apiServer) syncEmbeddedNPS() error {
	if !a.embedded {
		return nil
	}
	return integrated.SyncNPSState(a.store.Users(), filterTunnelsByNode(a.store.ListTunnels(""), core.DefaultNodeID))
}

func (a *apiServer) exportConfigs(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	users := a.store.Users()
	if err := os.MkdirAll(a.configOut, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written := []string{}
	npsClients, err := core.ExportNPSClients(users)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	npsClientsPath := filepath.Join(a.configOut, "nps-clients.json")
	if err := os.WriteFile(npsClientsPath, npsClients, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written = append(written, npsClientsPath)

	nodes := a.store.ListNodes()
	for _, user := range users {
		userDir := filepath.Join(a.configOut, "clients", user.Name)
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		tunnels := a.store.ListTunnels(user.Name)
		npc, err := core.RenderNPCCommand(user, a.runtime)
		if err == nil {
			path := filepath.Join(userDir, "npc-command.txt")
			if err := os.WriteFile(path, []byte(npc+"\n"), 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			written = append(written, path)
		}
		for _, node := range nodes {
			nodeTunnels := filterTunnelsByNode(tunnels, node.ID)
			if len(nodeTunnels) == 0 {
				continue
			}
			nodeDir := filepath.Join(userDir, node.ID)
			if err := os.MkdirAll(nodeDir, 0o755); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			cfg := runtimeForNode(node, a.runtime)
			npc, err := core.RenderNPCCommand(user, cfg)
			if err == nil {
				path := filepath.Join(nodeDir, "npc-command.txt")
				if err := os.WriteFile(path, []byte(npc+"\n"), 0o600); err != nil {
					writeError(w, http.StatusInternalServerError, err)
					return
				}
				written = append(written, path)
			}
		}
	}
	readme := filepath.Join(a.configOut, "README.txt")
	text := "Tunnel Control export\n\nnps-clients.json: managed NPS client list.\nclients/<user>/npc-command.txt: npc startup command for that user.\n"
	if err := os.WriteFile(readme, []byte(text), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written = append(written, readme)
	writeJSON(w, http.StatusOK, map[string]any{"status": "exported", "dir": a.configOut, "files": written})
}

func (a *apiServer) userConfig(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/users/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	reqUser := requestUser(r)
	if !isAdmin(reqUser) && reqUser.Name != parts[0] {
		writeErrorText(w, http.StatusForbidden, "cannot view another user's config")
		return
	}
	user, ok := a.store.GetUser(parts[0])
	if !ok {
		writeErrorText(w, http.StatusNotFound, "user not found")
		return
	}
	switch parts[1] {
	case "frpc.toml":
		tunnels := a.store.ListTunnels(user.Name)
		cfg := a.runtime
		if nodeID := r.URL.Query().Get("node"); nodeID != "" {
			node, ok := a.store.GetNode(nodeID)
			if !ok {
				writeErrorText(w, http.StatusNotFound, "node not found")
				return
			}
			cfg = runtimeForNode(node, a.runtime)
			tunnels = filterTunnelsByNode(tunnels, node.ID)
		}
		cfgText, err := core.RenderFRPC(*user, tunnels, cfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(cfgText))
	case "npc-command":
		cfg := a.runtime
		if nodeID := r.URL.Query().Get("node"); nodeID != "" {
			node, ok := a.store.GetNode(nodeID)
			if !ok {
				writeErrorText(w, http.StatusNotFound, "node not found")
				return
			}
			cfg = runtimeForNode(node, a.runtime)
		}
		cmd, err := core.RenderNPCCommand(*user, cfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(cmd + "\n"))
	case "tunnel-client":
		controlURL := requestBaseURL(r)
		text := renderTunnelClientHelp(user.Name, controlURL)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(text))
	case "nps-key":
		if user.NPSVerifyKey == "" {
			writeErrorText(w, http.StatusNotFound, "nps key not found")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(user.NPSVerifyKey + "\n"))
	default:
		http.NotFound(w, r)
	}
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := r.Host
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	return scheme + "://" + host
}

func renderTunnelClientHelp(userName, controlURL string) string {
	return fmt.Sprintf(`docker compose:

services:
  tunnel-client:
    image: darkver8/tunnel-port:latest
    container_name: tunnel-client
    restart: unless-stopped
    network_mode: host
    entrypoint: ["/usr/local/bin/tunnel-client"]
    environment:
      CONTROL_URL: %s
      TUNNEL_USER: %s
      TUNNEL_PASSWORD: 填写该用户的后台登录密码

直接运行:

tunnel-client -server %s -user %s -password 填写该用户的后台登录密码

说明:
用户只需要连接总控。tunnel-client 会自动从总控读取该用户的节点和 NPS 隧道，并自动连接对应节点。
`, controlURL, userName, controlURL, userName)
}

func (a *apiServer) exportFRPUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	b, err := core.ExportFRPUsers(a.store.Users())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(b, '\n'))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorText(w, status, err.Error())
}

func writeErrorText(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
