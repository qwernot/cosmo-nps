package main

import (
	"testing"
	"time"

	"qwernot/tunnel-control/internal/core"
	"qwernot/tunnel-control/internal/integrated"
)

func TestResourceUsageFor(t *testing.T) {
	users := []core.User{{
		Name:        "alice",
		PortPools:   []core.PortRange{{Start: 12000, End: 12002}},
		DomainPools: []string{"app.example.com"},
		MaxPorts:    3,
	}}
	tunnels := []core.Tunnel{
		{UserName: "alice", Mode: "tcp", RemotePort: 12000, Enabled: true},
		{UserName: "alice", Mode: "udp", RemotePort: 12000, Enabled: true},
		{UserName: "alice", Mode: "http", Domains: []string{"app.example.com"}, Enabled: true},
	}
	got := resourceUsageFor(users, tunnels)
	if len(got) != 1 {
		t.Fatalf("resourceUsageFor returned %d users, want 1", len(got))
	}
	usage := got[0]
	if usage.PortTotal != 3 || usage.TCPUsed != 1 || usage.TCPFree != 2 || usage.UDPUsed != 1 || usage.UDPFree != 2 {
		t.Fatalf("unexpected port usage: %+v", usage)
	}
	if usage.DomainTotal != 1 || usage.DomainUsed != 1 || usage.DomainFree != 0 {
		t.Fatalf("unexpected domain usage: %+v", usage)
	}
	if usage.TunnelUsed != 3 || usage.TunnelLimit != 3 || usage.TunnelFree != 0 {
		t.Fatalf("unexpected tunnel usage: %+v", usage)
	}
}

func TestClientStatusesFor(t *testing.T) {
	users := []core.User{{
		Name:         "alice",
		FRPToken:     "frp-token",
		NPSVerifyKey: "nps-key",
	}}
	tunnels := []core.Tunnel{
		{ID: "alice-frp-tcp-12000", UserName: "alice", Engine: core.EngineFRP, Mode: "tcp", Enabled: true},
		{ID: "alice-nps-tcp-12001", UserName: "alice", Engine: core.EngineNPS, Mode: "tcp", Enabled: true},
	}
	lastSeen := time.Date(2026, 6, 3, 8, 0, 0, 0, time.UTC)
	live := []integrated.ClientStatus{{
		UserName:   "alice",
		Engine:     core.EngineNPS,
		ClientID:   "2",
		ClientIP:   "192.0.2.10",
		Online:     true,
		LastSeenAt: lastSeen,
	}}

	got := clientStatusesFor(users, tunnels, true, live)
	if len(got) != 2 {
		t.Fatalf("clientStatusesFor returned %d statuses, want 2", len(got))
	}
	if got[0].Engine != core.EngineFRP || got[0].State != "offline" || got[0].TunnelTotal != 1 || got[0].TunnelOnline != 0 {
		t.Fatalf("unexpected frp status: %+v", got[0])
	}
	if got[1].Engine != core.EngineNPS || got[1].State != "online" || !got[1].Online || got[1].ClientIP != "192.0.2.10" || got[1].TunnelOnline != 1 {
		t.Fatalf("unexpected nps status: %+v", got[1])
	}
	if got[1].LastSeenAt != "2026-06-03T08:00:00Z" {
		t.Fatalf("unexpected lastSeenAt %q", got[1].LastSeenAt)
	}

	unknown := clientStatusesFor(users, nil, false, nil)
	if unknown[0].State != "unknown" || unknown[1].State != "unknown" {
		t.Fatalf("external mode should report unknown states: %+v", unknown)
	}
}

func TestSummarizeTunnelAvailability(t *testing.T) {
	tests := []struct {
		name        string
		clientState string
		entry       tunnelAvailabilityProbe
		wantState   string
	}{
		{name: "online and entry ok", clientState: "online", entry: tunnelAvailabilityProbe{State: "ok"}, wantState: "ok"},
		{name: "entry down wins", clientState: "online", entry: tunnelAvailabilityProbe{State: "down"}, wantState: "down"},
		{name: "client offline", clientState: "offline", entry: tunnelAvailabilityProbe{State: "ok"}, wantState: "down"},
		{name: "udp unknown with client online", clientState: "online", entry: tunnelAvailabilityProbe{State: "unknown"}, wantState: "warning"},
		{name: "external client unknown", clientState: "unknown", entry: tunnelAvailabilityProbe{State: "ok"}, wantState: "warning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := summarizeTunnelAvailability(tt.clientState, tt.entry)
			if got != tt.wantState {
				t.Fatalf("summarizeTunnelAvailability() state = %q, want %q", got, tt.wantState)
			}
		})
	}
}

func TestHTTPEntryPort(t *testing.T) {
	runtime := core.RuntimeConfig{
		FRPHTTPPort:      9081,
		FRPHTTPSPort:     9444,
		NPSHTTPProxyPort: 9080,
		NPSHTTPSPort:     9443,
	}
	if got := httpEntryPort(core.EngineFRP, "http", runtime); got != 9081 {
		t.Fatalf("frp http port = %d", got)
	}
	if got := httpEntryPort(core.EngineFRP, "https", runtime); got != 9444 {
		t.Fatalf("frp https port = %d", got)
	}
	if got := httpEntryPort(core.EngineNPS, "http", runtime); got != 9080 {
		t.Fatalf("nps http port = %d", got)
	}
	if got := httpEntryPort(core.EngineNPS, "https", runtime); got != 9443 {
		t.Fatalf("nps https port = %d", got)
	}
}

func TestClientStatusForTunnelMatchesNode(t *testing.T) {
	tunnel := core.Tunnel{UserName: "alice", NodeID: "edge-a", Engine: core.EngineNPS, Mode: "tcp", Enabled: true}
	live := []integrated.ClientStatus{
		{NodeID: core.DefaultNodeID, UserName: "alice", Engine: core.EngineNPS, Online: true},
		{NodeID: "edge-a", UserName: "alice", Engine: core.EngineNPS, Online: false},
	}
	got := clientStatusForTunnel(tunnel, live)
	if got.State != "unknown" || got.Online {
		t.Fatalf("remote offline status should not borrow local online client: %#v", got)
	}
	live[1].Online = true
	got = clientStatusForTunnel(tunnel, live)
	if got.State != "online" || !got.Online {
		t.Fatalf("expected edge client to be online: %#v", got)
	}
}

func TestRuntimeForNode(t *testing.T) {
	fallback := core.RuntimeConfig{
		ServerAddr:       "local.example.com",
		FRPServerPort:    17000,
		FRPHTTPPort:      9081,
		NPSServerPort:    18024,
		NPSHTTPProxyPort: 9080,
	}
	node := core.Node{
		PublicAddr: "edge.example.com",
		Runtime: core.RuntimeConfig{
			FRPServerPort: 17100,
			NPSServerPort: 18124,
		},
	}
	got := runtimeForNode(node, fallback)
	if got.ServerAddr != "edge.example.com" || got.FRPServerPort != 17100 || got.NPSServerPort != 18124 {
		t.Fatalf("unexpected node runtime: %#v", got)
	}
	if got.FRPHTTPPort != fallback.FRPHTTPPort || got.NPSHTTPProxyPort != fallback.NPSHTTPProxyPort {
		t.Fatalf("node runtime should keep fallback ports when not overridden: %#v", got)
	}
}

func TestProbeHost(t *testing.T) {
	tests := map[string]string{
		"":                          "127.0.0.1",
		"8.162.0.198":               "8.162.0.198",
		"8.162.0.198:18089":         "8.162.0.198",
		"http://node.example:18089": "node.example",
		"https://node.example/api":  "node.example",
	}
	for input, want := range tests {
		if got := probeHost(input); got != want {
			t.Fatalf("probeHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFilterTunnelsByNode(t *testing.T) {
	tunnels := []core.Tunnel{
		{ID: "legacy"},
		{ID: "local", NodeID: core.DefaultNodeID},
		{ID: "edge", NodeID: "edge-a"},
	}
	got := filterTunnelsByNode(tunnels, core.DefaultNodeID)
	if len(got) != 2 {
		t.Fatalf("local node should include legacy tunnels, got %#v", got)
	}
	got = filterTunnelsByNode(tunnels, "edge-a")
	if len(got) != 1 || got[0].ID != "edge" {
		t.Fatalf("unexpected edge tunnels: %#v", got)
	}
}
