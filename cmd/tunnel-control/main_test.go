package main

import (
	"testing"
	"time"

	"qwernot/tunnel-control/internal/core"
	"qwernot/tunnel-control/internal/integrated"
)

func TestPortFromListenAddr(t *testing.T) {
	tests := map[string]int{
		":8088":           8088,
		"127.0.0.1:18088": 18088,
		"8088":            8088,
		"bad":             0,
	}
	for input, want := range tests {
		if got := portFromListenAddr(input); got != want {
			t.Fatalf("portFromListenAddr(%q) = %d, want %d", input, got, want)
		}
	}
}

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
