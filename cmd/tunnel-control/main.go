package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qwernot/tunnel-control/internal/core"
	"qwernot/tunnel-control/internal/engine"
	"qwernot/tunnel-control/internal/integrated"
)

//go:embed web/*
var embeddedWeb embed.FS

type contextKey string

const userContextKey contextKey = "user"

func main() {
	var (
		addr             = flag.String("addr", ":8088", "HTTP listen address")
		dbPath           = flag.String("db", ".data/tunnel-control.json", "JSON database path")
		publicAddr       = flag.String("public-addr", "127.0.0.1", "public server address used in generated client configs")
		frpServerPort    = flag.Int("frp-port", 7000, "frps bind port used in generated frpc configs")
		frpHTTPPort      = flag.Int("frp-http-port", 0, "embedded FRP HTTP vhost port")
		frpHTTPSPort     = flag.Int("frp-https-port", 0, "embedded FRP HTTPS vhost port")
		npsServerPort    = flag.Int("nps-port", 8024, "nps bridge port used in generated npc commands")
		npsHTTPPort      = flag.Int("nps-http-port", 0, "NPS HTTP proxy port shown in runtime info")
		npsHTTPSPort     = flag.Int("nps-https-port", 0, "NPS HTTPS proxy port shown in runtime info")
		frpsBin          = flag.String("frps-bin", "", "optional frps binary path for engine start/stop")
		frpsConfig       = flag.String("frps-config", "", "optional frps config path used with -c")
		npsBin           = flag.String("nps-bin", "", "optional nps binary path for engine start/stop")
		npsWorkDir       = flag.String("nps-workdir", "", "optional nps working directory containing conf and web folders")
		embeddedEngines  = flag.Bool("embedded-engines", false, "run FRP and NPS engines in this process")
		frpDashboardPort = flag.Int("frp-dashboard-port", 0, "embedded FRP dashboard port; 0 disables the native dashboard")
		frpUsersPath     = flag.String("frp-users-path", ".data/frps-users.json", "path written by FRP userStore sync")
		npsClientsPath   = flag.String("nps-clients-path", "", "optional path written by NPS client sync")
		configOutDir     = flag.String("config-out-dir", ".data/export", "directory written by full config export")
		adminUser        = flag.String("admin-user", "admin", "bootstrap admin user if no enabled admin exists")
		adminPassword    = flag.String("admin-password", "admin123", "bootstrap admin password if no enabled admin exists")
	)
	flag.Parse()

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
		store:        store,
		sessions:     newSessionStore(),
		engines:      engine.NewManager(engine.Config{FRPSBin: *frpsBin, FRPSConfig: *frpsConfig, FRPSPort: *frpServerPort, NPSBin: *npsBin, NPSWorkDir: *npsWorkDir, NPSPort: *npsServerPort, Embedded: *embeddedEngines}),
		frpUserOut:   *frpUsersPath,
		npsClientOut: *npsClientsPath,
		configOut:    *configOutDir,
		embedded:     *embeddedEngines,
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
	if *embeddedEngines {
		go func() {
			if err := integrated.RunFRP(context.Background(), integrated.FRPOptions{
				BindPort:  *frpServerPort,
				HTTPPort:  *frpHTTPPort,
				HTTPSPort: *frpHTTPSPort,
				WebPort:   *frpDashboardPort,
				UserFile:  *frpUsersPath,
				Admin:     *adminUser,
				Password:  *adminPassword,
			}); err != nil {
				log.Printf("embedded frp stopped: %v", err)
			}
		}()
		go func() {
			if err := integrated.RunNPS(context.Background(), integrated.NPSOptions{
				WorkDir:    *npsWorkDir,
				BridgePort: *npsServerPort,
			}); err != nil {
				log.Printf("embedded nps stopped: %v", err)
			}
		}()
		go func() {
			time.Sleep(2 * time.Second)
			if err := api.syncEmbeddedAll(); err != nil {
				log.Printf("embedded engine state sync failed: %v", err)
			}
		}()
	}

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
	mux.HandleFunc("GET /api/tunnels", api.listTunnels)
	mux.HandleFunc("POST /api/tunnels", api.createTunnel)
	mux.HandleFunc("PUT /api/tunnels/", api.updateTunnel)
	mux.HandleFunc("DELETE /api/tunnels/", api.deleteTunnel)
	mux.HandleFunc("GET /api/runtime", api.runtimeInfo)
	mux.HandleFunc("GET /api/engines", api.engineStatuses)
	mux.HandleFunc("POST /api/engines/", api.engineAction)
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
	store        *core.Store
	sessions     *sessionStore
	engines      *engine.Manager
	frpUserOut   string
	npsClientOut string
	configOut    string
	embedded     bool
	runtime      core.RuntimeConfig
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
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/login" {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *apiServer) runtimeInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"client":         a.runtime,
		"engines":        a.engines.Config(),
		"frpUsersPath":   a.frpUserOut,
		"npsClientsPath": a.npsClientOut,
		"configOutDir":   a.configOut,
	})
}

func (a *apiServer) engineStatuses(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, a.engines.Statuses())
}

func (a *apiServer) engineAction(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/engines/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	var (
		st  engine.Status
		err error
	)
	switch parts[1] {
	case "start":
		st, err = a.engines.Start(parts[0])
	case "stop":
		st, err = a.engines.Stop(parts[0])
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
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
	return integrated.SyncNPSState(a.store.Users(), a.store.ListTunnels(""))
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
	frpUsers, err := core.ExportFRPUsers(users)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written := []string{}
	frpUsersPath := filepath.Join(a.configOut, "frp-users.json")
	if err := os.WriteFile(frpUsersPath, append(frpUsers, '\n'), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written = append(written, frpUsersPath)
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

	for _, user := range users {
		userDir := filepath.Join(a.configOut, "clients", user.Name)
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		tunnels := a.store.ListTunnels(user.Name)
		frpc, err := core.RenderFRPC(user, tunnels, a.runtime)
		if err == nil {
			path := filepath.Join(userDir, "frpc.toml")
			if err := os.WriteFile(path, []byte(frpc), 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			written = append(written, path)
		}
		npc, err := core.RenderNPCCommand(user, a.runtime)
		if err == nil {
			path := filepath.Join(userDir, "npc-command.txt")
			if err := os.WriteFile(path, []byte(npc+"\n"), 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			written = append(written, path)
		}
	}
	readme := filepath.Join(a.configOut, "README.txt")
	text := "Tunnel Control export\n\nfrp-users.json: copy or sync to frps userStore path.\nclients/<user>/frpc.toml: frpc client config for that user.\nclients/<user>/npc-command.txt: npc startup command for that user.\n"
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
		cfg, err := core.RenderFRPC(*user, a.store.ListTunnels(user.Name), a.runtime)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(cfg))
	case "npc-command":
		cmd, err := core.RenderNPCCommand(*user, a.runtime)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(cmd + "\n"))
	default:
		http.NotFound(w, r)
	}
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
