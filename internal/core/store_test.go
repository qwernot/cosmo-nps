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
		UserName: "alice", Engine: EngineFRP, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
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
		UserName: "alice", Engine: EngineFRP, Mode: "tcp", RemotePort: 10000, LocalPort: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineFRP, Mode: "udp", RemotePort: 10000, LocalPort: 8081,
	}); err != nil {
		t.Fatalf("tcp and udp should be allowed to share the same remote port: %v", err)
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineNPS, Mode: "socks5", RemotePort: 10000, LocalPort: 8082,
	}); err == nil {
		t.Fatal("expected socks5 to conflict with tcp on the same remote port")
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
		UserName: "alice", Engine: EngineFRP, Mode: "https", LocalPort: 8081, Domains: []string{"bad.example.net"},
	}); err == nil {
		t.Fatal("expected domain pool validation error")
	}
	if _, err := store.CreateTunnel(Tunnel{
		UserName: "alice", Engine: EngineFRP, Mode: "https", LocalPort: 8082, Domains: []string{"web.example.com"},
	}); err == nil {
		t.Fatal("expected duplicate domain validation error")
	}
}

func TestRenderConfigs(t *testing.T) {
	user := User{Name: "alice", FRPToken: "frp-token", NPSVerifyKey: "nps-key"}
	tunnels := []Tunnel{{
		ID: "web", UserName: "alice", Engine: EngineFRP, Mode: "tcp", RemotePort: 10000,
		LocalIP: "127.0.0.1", LocalPort: 8080, Enabled: true,
	}}
	frpc, err := RenderFRPC(user, tunnels, RuntimeConfig{ServerAddr: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`user = "alice"`, `token = "frp-token"`, `remotePort = 10000`} {
		if !strings.Contains(frpc, want) {
			t.Fatalf("frpc config missing %q:\n%s", want, frpc)
		}
	}
	cmd, err := RenderNPCCommand(user, RuntimeConfig{ServerAddr: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "./npc -server=example.com:8024 -vkey=nps-key" {
		t.Fatalf("unexpected npc command: %s", cmd)
	}
}

func TestRenderFRPHTTPConfig(t *testing.T) {
	user := User{Name: "alice", FRPToken: "frp-token"}
	tunnels := []Tunnel{{
		ID: "web", UserName: "alice", Engine: EngineFRP, Mode: "http", Domains: []string{"web.example.com"},
		LocalIP: "127.0.0.1", LocalPort: 8080, Enabled: true,
	}}
	frpc, err := RenderFRPC(user, tunnels, RuntimeConfig{ServerAddr: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`type = "http"`, `customDomains = ["web.example.com"]`, `localPort = 8080`} {
		if !strings.Contains(frpc, want) {
			t.Fatalf("frpc config missing %q:\n%s", want, frpc)
		}
	}
	if strings.Contains(frpc, "remotePort") {
		t.Fatalf("http proxy should not render remotePort:\n%s", frpc)
	}
}
