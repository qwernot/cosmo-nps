package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePortRanges(t *testing.T) {
	ranges, err := ParsePortRanges("10000-10002,10010")
	if err != nil {
		t.Fatal(err)
	}
	if !ranges[0].Contains(10001) || ranges[0].Contains(9999) || !ranges[1].Contains(10010) {
		t.Fatalf("unexpected ranges: %#v", ranges)
	}
	if got := FormatPortRanges(ranges); got != "10000-10002,10010" {
		t.Fatalf("FormatPortRanges() = %q", got)
	}
}

func TestCreateTunnelValidatesPortPoolAndReuse(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := ParsePortRanges("10000-10001")
	if _, err := store.UpsertUser(User{Name: "alice", Role: RoleUser, Enabled: true, PortPools: pool, MaxPorts: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "tcp", RemotePort: 10002, LocalPort: 8081,
	}); err == nil {
		t.Fatal("expected port pool validation error")
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "tcp", RemotePort: 10001, LocalPort: 8082,
	}); err == nil {
		t.Fatal("expected max port validation error")
	}
}

func TestCreateTunnelAllowsSamePortAcrossTCPAndUDP(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := ParsePortRanges("10000")
	if _, err := store.UpsertUser(User{Name: "alice", Role: RoleUser, Enabled: true, PortPools: pool, MaxPorts: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "udp", RemotePort: 10000, LocalPort: 8081,
	}); err != nil {
		t.Fatalf("tcp and udp should be allowed to share the same remote port: %v", err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "socks5", RemotePort: 10000, LocalPort: 8082,
	}); err == nil {
		t.Fatal("expected socks5 to conflict with tcp on the same remote port")
	}
}

func TestStoreCreatesDefaultLocalNode(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := store.GetNode(DefaultNodeID)
	if !ok {
		t.Fatal("default local node was not created")
	}
	if !node.Enabled || node.FRPEnabled || !node.NPSEnabled {
		t.Fatalf("default local node should enable only nps: %#v", node)
	}
}

func TestCreateTunnelAllowsSameRemotePortAcrossNodes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := ParsePortRanges("10000")
	if _, err := store.UpsertUser(User{Name: "alice", Role: RoleUser, Enabled: true, PortPools: pool, MaxPorts: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(Node{
		ID: "edge-a", Name: "Edge A", Enabled: true, NPSEnabled: true, PortPools: pool,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", NodeID: DefaultNodeID, Engine: EngineNPS, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	tunnel, err := store.CreateTunnel(Tunnel{
		UserName: "alice", NodeID: "edge-a", Engine: EngineNPS, Mode: "tcp", RemotePort: 10000, LocalPort: 8081,
	})
	if err != nil {
		t.Fatalf("same tcp port should be allowed on a different node: %v", err)
	}
	if tunnel.NodeID != "edge-a" || !strings.Contains(tunnel.ID, "edge-a") {
		t.Fatalf("expected node scoped tunnel id and node id, got %#v", tunnel)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", NodeID: "edge-a", Engine: EngineNPS, Mode: "socks5", RemotePort: 10000, LocalPort: 8082,
	}); err == nil {
		t.Fatal("expected tcp-family port conflict inside the same node")
	}
}

func TestCreateTunnelRejectsFRPEngine(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := ParsePortRanges("10000")
	if _, err := store.UpsertUser(User{Name: "alice", Role: RoleUser, Enabled: true, PortPools: pool, MaxPorts: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(Node{
		ID: "edge-a", Name: "Edge A", Enabled: true, NPSEnabled: true, PortPools: pool,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", NodeID: "edge-a", Engine: EngineFRP, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
	}); err == nil {
		t.Fatal("expected frp engine validation error")
	}
}

func TestUpdateNodeStatus(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(Node{ID: "edge-a", Name: "Edge A", Enabled: true, FRPEnabled: true, NPSEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeStatus("edge-a", true, ""); err != nil {
		t.Fatal(err)
	}
	node, ok := store.GetNode("edge-a")
	if !ok {
		t.Fatal("node not found")
	}
	if !node.Status.Online || node.Status.LastSeenAt.IsZero() || node.Status.LastSyncAt.IsZero() || node.Status.LastError != "" {
		t.Fatalf("unexpected online status: %#v", node.Status)
	}
	if err := store.UpdateNodeStatus("edge-a", false, "connection refused"); err != nil {
		t.Fatal(err)
	}
	node, _ = store.GetNode("edge-a")
	if node.Status.Online || node.Status.LastError != "connection refused" {
		t.Fatalf("unexpected offline status: %#v", node.Status)
	}
}

func TestCreateHTTPSTunnelValidatesDomainPool(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	domains, err := ParseDomainPools("*.example.com,app.test.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertUser(User{Name: "alice", Role: RoleUser, Enabled: true, DomainPools: domains, MaxPorts: 2}); err != nil {
		t.Fatal(err)
	}
	tunnel, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "https", RemotePort: 10000, LocalPort: 8080, Domains: []string{"web.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.RemotePort != 0 || strings.Join(tunnel.Domains, ",") != "web.example.com" {
		t.Fatalf("unexpected normalized tunnel: %#v", tunnel)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "https", LocalPort: 8081, Domains: []string{"bad.example.net"},
	}); err == nil {
		t.Fatal("expected domain pool validation error")
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "https", LocalPort: 8082, Domains: []string{"web.example.com"},
	}); err == nil {
		t.Fatal("expected duplicate domain validation error")
	}
}

func TestRenderConfigs(t *testing.T) {
	user := User{Name: "alice", NPSVerifyKey: "nps-key"}
	cmd, err := RenderNPCCommand(user, RuntimeConfig{ServerAddr: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "./npc -server=example.com:8024 -vkey=nps-key" {
		t.Fatalf("unexpected npc command: %s", cmd)
	}
}
