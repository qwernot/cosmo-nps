package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.RWMutex
	db   Database
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		db: Database{
			Users:   map[string]*User{},
			Nodes:   map[string]*Node{},
			Tunnels: map[string]*Tunnel{},
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if err := s.migrateDefaultNode(); err != nil {
		return nil, err
	}
	if err := s.migrateLegacyPasswords(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) EnsureAdmin(name, password string) (bool, error) {
	if name == "" {
		return false, fmt.Errorf("admin name is required")
	}
	if password == "" {
		return false, fmt.Errorf("admin password is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.db.Users {
		if u.Role == RoleAdmin && u.Enabled {
			return false, nil
		}
	}
	u := &User{
		Name:         name,
		PasswordHash: hashPassword(password),
		Role:         RoleAdmin,
		Enabled:      true,
		FRPToken:     randomSecret(),
		NPSVerifyKey: randomSecret(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.db.Users[name] = u
	return true, s.saveLocked()
}

func (s *Store) ListUsers() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]PublicUser, 0, len(s.db.Users))
	for _, u := range s.db.Users {
		users = append(users, Public(u))
	}
	slices.SortFunc(users, func(a, b PublicUser) int {
		return strings.Compare(a.Name, b.Name)
	})
	return users
}

func (s *Store) GetUser(name string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.db.Users[name]
	if !ok {
		return nil, false
	}
	cp := *u
	cp.PortPools = append([]PortRange(nil), u.PortPools...)
	cp.DomainPools = append([]string(nil), u.DomainPools...)
	return &cp, true
}

func (s *Store) VerifyLogin(name, password string) (*User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.db.Users[name]
	if !ok || !u.Enabled || !verifyPassword(u, password) {
		return nil, false
	}
	if u.Password != "" {
		u.PasswordHash = hashPassword(password)
		u.Password = ""
		u.UpdatedAt = time.Now().UTC()
		_ = s.saveLocked()
	}
	cp := *u
	cp.PortPools = append([]PortRange(nil), u.PortPools...)
	cp.DomainPools = append([]string(nil), u.DomainPools...)
	return &cp, true
}

func (s *Store) SetPassword(name, password string) error {
	if name == "" {
		return fmt.Errorf("user name is required")
	}
	if password == "" {
		return fmt.Errorf("password is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.db.Users[name]
	if !ok {
		return fmt.Errorf("user %q not found", name)
	}
	u.Password = ""
	u.PasswordHash = hashPassword(password)
	u.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

func (s *Store) Users() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.db.Users))
	for _, u := range s.db.Users {
		cp := *u
		cp.PortPools = append([]PortRange(nil), u.PortPools...)
		cp.DomainPools = append([]string(nil), u.DomainPools...)
		users = append(users, cp)
	}
	slices.SortFunc(users, func(a, b User) int {
		return strings.Compare(a.Name, b.Name)
	})
	return users
}

func (s *Store) ListNodes() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]Node, 0, len(s.db.Nodes))
	for _, node := range s.db.Nodes {
		nodes = append(nodes, cloneNode(node))
	}
	slices.SortFunc(nodes, func(a, b Node) int {
		return strings.Compare(a.ID, b.ID)
	})
	return nodes
}

func (s *Store) GetNode(id string) (Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node, ok := s.db.Nodes[normalizeID(id)]
	if !ok {
		return Node{}, false
	}
	return cloneNode(node), true
}

func (s *Store) UpsertNode(in Node) (Node, error) {
	in.ID = normalizeID(in.ID)
	if in.ID == "" {
		return Node{}, fmt.Errorf("node id is required")
	}
	if in.Name == "" {
		in.Name = in.ID
	}
	domains, err := NormalizeDomains(in.DomainPools)
	if err != nil {
		return Node{}, err
	}
	in.DomainPools = domains
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.db.Nodes[in.ID]
	if !ok {
		node = &Node{ID: in.ID, CreatedAt: now}
		s.db.Nodes[in.ID] = node
	}
	node.Name = in.Name
	if in.Token != "" {
		node.Token = in.Token
	} else if node.Token == "" {
		node.Token = randomSecret()
	}
	node.PublicAddr = strings.TrimSpace(in.PublicAddr)
	node.Enabled = in.Enabled
	node.FRPEnabled = false
	node.NPSEnabled = in.NPSEnabled
	node.PortPools = append([]PortRange(nil), in.PortPools...)
	node.DomainPools = append([]string(nil), in.DomainPools...)
	node.Runtime = in.Runtime
	node.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return Node{}, err
	}
	return cloneNode(node), nil
}

func (s *Store) DeleteNode(id string) error {
	id = normalizeID(id)
	if id == "" {
		return fmt.Errorf("node id is required")
	}
	if id == DefaultNodeID {
		return fmt.Errorf("cannot delete default node")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.db.Nodes[id]; !ok {
		return nil
	}
	for _, tunnel := range s.db.Tunnels {
		if tunnelNodeID(tunnel) == id {
			return fmt.Errorf("node %q is used by tunnel %s", id, tunnel.ID)
		}
	}
	delete(s.db.Nodes, id)
	return s.saveLocked()
}

func (s *Store) UpdateNodeStatus(id string, online bool, lastError string) error {
	id = normalizeID(id)
	if id == "" {
		return fmt.Errorf("node id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.db.Nodes[id]
	if !ok {
		return fmt.Errorf("node %q not found", id)
	}
	node.Status.Online = online
	node.Status.LastSyncAt = now
	if online {
		node.Status.LastSeenAt = now
		node.Status.LastError = ""
	} else {
		node.Status.LastError = strings.TrimSpace(lastError)
		if len(node.Status.LastError) > 500 {
			node.Status.LastError = node.Status.LastError[:500]
		}
	}
	return s.saveLocked()
}

func (s *Store) UpsertUser(in User) (PublicUser, error) {
	if in.Name == "" {
		return PublicUser{}, fmt.Errorf("user name is required")
	}
	if in.Role == "" {
		in.Role = RoleUser
	}
	if in.Role != RoleAdmin && in.Role != RoleUser {
		return PublicUser{}, fmt.Errorf("role must be %q or %q", RoleAdmin, RoleUser)
	}
	domains, err := NormalizeDomains(in.DomainPools)
	if err != nil {
		return PublicUser{}, err
	}
	in.DomainPools = domains
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.db.Users[in.Name]
	if !ok {
		u = &User{
			Name:      in.Name,
			Enabled:   true,
			CreatedAt: now,
		}
		s.db.Users[in.Name] = u
	}
	u.Role = in.Role
	u.Enabled = in.Enabled
	u.PortPools = append([]PortRange(nil), in.PortPools...)
	u.DomainPools = append([]string(nil), in.DomainPools...)
	u.MaxPorts = in.MaxPorts
	if in.Password != "" {
		u.Password = ""
		u.PasswordHash = hashPassword(in.Password)
	}
	if in.FRPToken != "" {
		u.FRPToken = in.FRPToken
	} else if u.FRPToken == "" {
		u.FRPToken = randomSecret()
	}
	if in.NPSVerifyKey != "" {
		u.NPSVerifyKey = in.NPSVerifyKey
	} else if u.NPSVerifyKey == "" {
		u.NPSVerifyKey = randomSecret()
	}
	u.RateLimit = in.RateLimit
	u.FlowLimit = in.FlowLimit
	u.FlowUsed = in.FlowUsed
	u.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return PublicUser{}, err
	}
	return Public(u), nil
}

func (s *Store) DeleteUser(name string) error {
	if name == "" {
		return fmt.Errorf("user name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.db.Users[name]
	if !ok {
		return nil
	}
	if u.Role == RoleAdmin {
		admins := 0
		for _, other := range s.db.Users {
			if other.Role == RoleAdmin && other.Enabled {
				admins++
			}
		}
		if admins <= 1 {
			return fmt.Errorf("cannot delete the last enabled admin")
		}
	}
	for id, t := range s.db.Tunnels {
		if t.UserName == name {
			delete(s.db.Tunnels, id)
		}
	}
	delete(s.db.Users, name)
	return s.saveLocked()
}

func (s *Store) ListTunnels(userName string) []Tunnel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tunnel, 0, len(s.db.Tunnels))
	for _, t := range s.db.Tunnels {
		if userName != "" && t.UserName != userName {
			continue
		}
		out = append(out, *t)
	}
	slices.SortFunc(out, func(a, b Tunnel) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (s *Store) CreateTunnel(in Tunnel) (Tunnel, error) {
	in.NodeID = normalizeID(in.NodeID)
	if in.NodeID == "" {
		in.NodeID = DefaultNodeID
	}
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	in.Engine = strings.ToLower(strings.TrimSpace(in.Engine))
	if in.LocalIP == "" {
		in.LocalIP = "127.0.0.1"
	}
	domains, err := NormalizeDomains(in.Domains)
	if err != nil {
		return Tunnel{}, err
	}
	in.Domains = domains
	if isDomainMode(in.Mode) {
		in.RemotePort = 0
	} else {
		in.Domains = nil
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateTunnelLocked(in, ""); err != nil {
		return Tunnel{}, err
	}
	if in.ID == "" {
		in.ID = nextTunnelID(in.UserName, in.NodeID, in.Engine, in.Mode, in.RemotePort)
	}
	if _, exists := s.db.Tunnels[in.ID]; exists {
		return Tunnel{}, fmt.Errorf("tunnel %q already exists", in.ID)
	}
	in.CreatedAt = now
	in.UpdatedAt = now
	cp := in
	cp.Domains = append([]string(nil), in.Domains...)
	s.db.Tunnels[in.ID] = &cp
	if err := s.saveLocked(); err != nil {
		return Tunnel{}, err
	}
	return cp, nil
}

func (s *Store) UpdateTunnel(id string, in Tunnel) (Tunnel, error) {
	in.NodeID = normalizeID(in.NodeID)
	if in.NodeID == "" {
		in.NodeID = DefaultNodeID
	}
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	in.Engine = strings.ToLower(strings.TrimSpace(in.Engine))
	if in.LocalIP == "" {
		in.LocalIP = "127.0.0.1"
	}
	domains, err := NormalizeDomains(in.Domains)
	if err != nil {
		return Tunnel{}, err
	}
	in.Domains = domains
	if isDomainMode(in.Mode) {
		in.RemotePort = 0
	} else {
		in.Domains = nil
	}
	if id == "" {
		return Tunnel{}, fmt.Errorf("tunnel id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.db.Tunnels[id]
	if !ok {
		return Tunnel{}, fmt.Errorf("tunnel %q not found", id)
	}
	if in.ID == "" {
		in.ID = id
	}
	if in.ID != id {
		return Tunnel{}, fmt.Errorf("tunnel id cannot be changed")
	}
	if err := s.validateTunnelLocked(in, id); err != nil {
		return Tunnel{}, err
	}
	in.CreatedAt = existing.CreatedAt
	in.UpdatedAt = now
	cp := in
	cp.Domains = append([]string(nil), in.Domains...)
	s.db.Tunnels[id] = &cp
	if err := s.saveLocked(); err != nil {
		return Tunnel{}, err
	}
	return cp, nil
}

func (s *Store) DeleteTunnel(id string) error {
	if id == "" {
		return fmt.Errorf("tunnel id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.db.Tunnels, id)
	return s.saveLocked()
}

func (s *Store) validateTunnelLocked(in Tunnel, existingID string) error {
	if in.UserName == "" {
		return fmt.Errorf("userName is required")
	}
	if in.Engine != EngineNPS {
		return fmt.Errorf("engine must be %q", EngineNPS)
	}
	node, ok := s.db.Nodes[in.NodeID]
	if !ok || !node.Enabled {
		return fmt.Errorf("node %q not found or disabled", in.NodeID)
	}
	if in.Engine == EngineNPS && !node.NPSEnabled {
		return fmt.Errorf("node %q does not enable nps", in.NodeID)
	}
	if in.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	if !isPortMode(in.Mode) && !isDomainMode(in.Mode) {
		return fmt.Errorf("mode must be tcp, udp, socks5, http or https")
	}
	if in.LocalPort <= 0 || in.LocalPort > 65535 {
		return fmt.Errorf("local port is required")
	}
	u, ok := s.db.Users[in.UserName]
	if !ok || !u.Enabled {
		return fmt.Errorf("user %q not found or disabled", in.UserName)
	}
	if isPortMode(in.Mode) {
		if in.RemotePort <= 0 {
			return fmt.Errorf("remote port is required for %s tunnel", in.Mode)
		}
		if !portInRanges(in.RemotePort, u.PortPools) {
			return fmt.Errorf("remote port %d is outside user port pool %s", in.RemotePort, FormatPortRanges(u.PortPools))
		}
		if len(node.PortPools) > 0 && !portInRanges(in.RemotePort, node.PortPools) {
			return fmt.Errorf("remote port %d is outside node %s port pool %s", in.RemotePort, in.NodeID, FormatPortRanges(node.PortPools))
		}
		for _, existing := range s.db.Tunnels {
			if existing.ID == existingID || !isPortMode(existing.Mode) {
				continue
			}
			if tunnelNodeID(existing) != in.NodeID {
				continue
			}
			if existing.RemotePort == in.RemotePort && portTransport(existing.Mode) == portTransport(in.Mode) {
				return fmt.Errorf("%s remote port %d is already used by tunnel %s", portTransport(in.Mode), in.RemotePort, existing.ID)
			}
		}
		in.Domains = nil
	} else {
		if len(in.Domains) == 0 {
			return fmt.Errorf("domain is required for %s tunnel", in.Mode)
		}
		for _, domain := range in.Domains {
			if !domainInPools(domain, u.DomainPools) {
				return fmt.Errorf("domain %s is outside user domain pool %s", domain, FormatDomainPools(u.DomainPools))
			}
			if len(node.DomainPools) > 0 && !domainInPools(domain, node.DomainPools) {
				return fmt.Errorf("domain %s is outside node %s domain pool %s", domain, in.NodeID, FormatDomainPools(node.DomainPools))
			}
			for _, existing := range s.db.Tunnels {
				if existing.ID == existingID || !isDomainMode(existing.Mode) {
					continue
				}
				for _, existingDomain := range existing.Domains {
					if existing.Mode == in.Mode && existingDomain == domain {
						return fmt.Errorf("domain %s is already used by tunnel %s", domain, existing.ID)
					}
				}
			}
		}
	}
	if u.MaxPorts > 0 && s.countUserTunnelsLocked(in.UserName, existingID) >= u.MaxPorts {
		return fmt.Errorf("user %q reached max tunnel count %d", in.UserName, u.MaxPorts)
	}
	return nil
}

func (s *Store) countUserTunnelsLocked(userName, exceptID string) int {
	count := 0
	for _, t := range s.db.Tunnels {
		if t.ID != exceptID && t.UserName == userName {
			count++
		}
	}
	return count
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &s.db); err != nil {
		return err
	}
	if s.db.Users == nil {
		s.db.Users = map[string]*User{}
	}
	if s.db.Nodes == nil {
		s.db.Nodes = map[string]*Node{}
	}
	if s.db.Tunnels == nil {
		s.db.Tunnels = map[string]*Tunnel{}
	}
	return nil
}

func (s *Store) migrateDefaultNode() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	now := time.Now().UTC()
	if s.db.Nodes == nil {
		s.db.Nodes = map[string]*Node{}
		changed = true
	}
	if _, ok := s.db.Nodes[DefaultNodeID]; !ok {
		s.db.Nodes[DefaultNodeID] = &Node{
			ID:         DefaultNodeID,
			Name:       "Local Node",
			Token:      randomSecret(),
			Enabled:    true,
			FRPEnabled: false,
			NPSEnabled: true,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		changed = true
	}
	for _, tunnel := range s.db.Tunnels {
		if tunnel.NodeID == "" {
			tunnel.NodeID = DefaultNodeID
			tunnel.UpdatedAt = now
			changed = true
		}
	}
	for _, node := range s.db.Nodes {
		if node.Token == "" {
			node.Token = randomSecret()
			node.UpdatedAt = now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) migrateLegacyPasswords() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, u := range s.db.Users {
		if u.Password != "" && u.PasswordHash == "" {
			u.PasswordHash = hashPassword(u.Password)
			u.Password = ""
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(b, '\n'), 0o600)
}

func cloneNode(node *Node) Node {
	cp := *node
	cp.PortPools = append([]PortRange(nil), node.PortPools...)
	cp.DomainPools = append([]string(nil), node.DomainPools...)
	return cp
}

func tunnelNodeID(tunnel *Tunnel) string {
	if tunnel == nil || tunnel.NodeID == "" {
		return DefaultNodeID
	}
	return tunnel.NodeID
}

func normalizeID(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

func portInRanges(port int, ranges []PortRange) bool {
	for _, r := range ranges {
		if r.Contains(port) {
			return true
		}
	}
	return false
}

func domainInPools(domain string, pools []string) bool {
	for _, pool := range pools {
		if pool == "*" || pool == domain {
			return true
		}
		if strings.HasPrefix(pool, "*.") {
			suffix := strings.TrimPrefix(pool, "*.")
			if domain != suffix && strings.HasSuffix(domain, "."+suffix) {
				return true
			}
		}
	}
	return false
}

func isPortMode(mode string) bool {
	switch mode {
	case "tcp", "udp", "socks5":
		return true
	default:
		return false
	}
}

func isDomainMode(mode string) bool {
	return mode == "http" || mode == "https"
}

func portTransport(mode string) string {
	if mode == "udp" {
		return "udp"
	}
	return "tcp"
}

func nextTunnelID(userName, nodeID, engine, mode string, port int) string {
	prefix := fmt.Sprintf("%s-%s-%s", userName, engine, mode)
	if nodeID != "" && nodeID != DefaultNodeID {
		prefix = fmt.Sprintf("%s-%s-%s-%s", userName, nodeID, engine, mode)
	}
	if port > 0 {
		return fmt.Sprintf("%s-%d", prefix, port)
	}
	return fmt.Sprintf("%s-%s", prefix, randomSecret()[:8])
}

func randomSecret() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func hashPassword(password string) string {
	salt := randomSecret()
	const iterations = 100000
	sum := passwordDigest(password, salt, iterations)
	return fmt.Sprintf("sha256:%d:%s:%s", iterations, salt, hex.EncodeToString(sum))
}

func verifyPassword(u *User, password string) bool {
	if u.PasswordHash != "" {
		parts := strings.Split(u.PasswordHash, ":")
		if len(parts) != 4 || parts[0] != "sha256" {
			return false
		}
		iterations, err := strconv.Atoi(parts[1])
		if err != nil || iterations <= 0 {
			return false
		}
		expected, err := hex.DecodeString(parts[3])
		if err != nil {
			return false
		}
		actual := passwordDigest(password, parts[2], iterations)
		return subtle.ConstantTimeCompare(expected, actual) == 1
	}
	return u.Password != "" && subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) == 1
}

func passwordDigest(password, salt string, iterations int) []byte {
	buf := []byte(password + ":" + salt)
	for i := 0; i < iterations; i++ {
		sum := sha256.Sum256(buf)
		buf = sum[:]
	}
	return buf
}
