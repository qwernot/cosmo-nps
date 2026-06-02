package main

import (
	"testing"

	"qwernot/tunnel-control/internal/core"
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
