package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type RuntimeConfig struct {
	ServerAddr    string `json:"serverAddr"`
	FRPServerPort int    `json:"frpServerPort"`
	NPSServerPort int    `json:"npsServerPort"`
}

func (c RuntimeConfig) withDefaults() RuntimeConfig {
	if c.ServerAddr == "" {
		c.ServerAddr = "127.0.0.1"
	}
	if c.FRPServerPort == 0 {
		c.FRPServerPort = 7000
	}
	if c.NPSServerPort == 0 {
		c.NPSServerPort = 8024
	}
	return c
}

func RenderFRPC(user User, tunnels []Tunnel, cfg RuntimeConfig) (string, error) {
	cfg = cfg.withDefaults()
	if user.FRPToken == "" {
		return "", fmt.Errorf("user %q has no frp token", user.Name)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "user = %q\n", user.Name)
	fmt.Fprintf(&b, "serverAddr = %q\n", cfg.ServerAddr)
	fmt.Fprintf(&b, "serverPort = %d\n", cfg.FRPServerPort)
	fmt.Fprintf(&b, "loginFailExit = true\n\n")
	fmt.Fprintf(&b, "[auth]\n")
	fmt.Fprintf(&b, "method = %q\n", "token")
	fmt.Fprintf(&b, "token = %q\n", user.FRPToken)
	for _, t := range tunnels {
		if t.Engine != EngineFRP || !t.Enabled {
			continue
		}
		fmt.Fprintf(&b, "\n[[proxies]]\n")
		fmt.Fprintf(&b, "name = %q\n", t.ID)
		fmt.Fprintf(&b, "type = %q\n", t.Mode)
		if len(t.Domains) > 0 {
			fmt.Fprintf(&b, "customDomains = [%s]\n", quoteList(t.Domains))
		}
		if t.LocalIP != "" {
			fmt.Fprintf(&b, "localIP = %q\n", t.LocalIP)
		}
		if t.LocalPort > 0 {
			fmt.Fprintf(&b, "localPort = %d\n", t.LocalPort)
		}
		if t.RemotePort > 0 {
			fmt.Fprintf(&b, "remotePort = %d\n", t.RemotePort)
		}
	}
	return b.String(), nil
}

func RenderNPCCommand(user User, cfg RuntimeConfig) (string, error) {
	cfg = cfg.withDefaults()
	if user.NPSVerifyKey == "" {
		return "", fmt.Errorf("user %q has no nps verify key", user.Name)
	}
	return fmt.Sprintf("./npc -server=%s:%d -vkey=%s", cfg.ServerAddr, cfg.NPSServerPort, user.NPSVerifyKey), nil
}

func ExportFRPUsers(users []User) ([]byte, error) {
	type frpUser struct {
		Name       string      `json:"name"`
		Password   string      `json:"password,omitempty"`
		Token      string      `json:"token,omitempty"`
		Role       string      `json:"role,omitempty"`
		Enabled    bool        `json:"enabled"`
		AllowPorts []PortRange `json:"allowPorts,omitempty"`
		MaxPorts   int         `json:"maxPorts,omitempty"`
	}
	out := make([]frpUser, 0, len(users))
	for _, u := range users {
		out = append(out, frpUser{
			Name:       u.Name,
			Password:   u.Password,
			Token:      u.FRPToken,
			Role:       u.Role,
			Enabled:    u.Enabled,
			AllowPorts: append([]PortRange(nil), u.PortPools...),
			MaxPorts:   u.MaxPorts,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return json.MarshalIndent(out, "", "  ")
}

func ExportNPSClients(users []User) ([]byte, error) {
	type npsClient struct {
		Cnf             map[string]any `json:"Cnf"`
		ID              int            `json:"Id"`
		VerifyKey       string         `json:"VerifyKey"`
		Addr            string         `json:"Addr"`
		Remark          string         `json:"Remark"`
		Status          bool           `json:"Status"`
		IsConnect       bool           `json:"IsConnect"`
		RateLimit       int            `json:"RateLimit"`
		Flow            map[string]int `json:"Flow"`
		Rate            map[string]int `json:"Rate"`
		NoStore         bool           `json:"NoStore"`
		NoDisplay       bool           `json:"NoDisplay"`
		MaxConn         int            `json:"MaxConn"`
		NowConn         int            `json:"NowConn"`
		WebUserName     string         `json:"WebUserName"`
		WebPassword     string         `json:"WebPassword"`
		PortPool        string         `json:"PortPool"`
		ConfigConnAllow bool           `json:"ConfigConnAllow"`
		MaxTunnelNum    int            `json:"MaxTunnelNum"`
		Version         string         `json:"Version"`
		BlackIPList     []string       `json:"BlackIpList"`
		CreateTime      string         `json:"CreateTime"`
		LastOnlineTime  string         `json:"LastOnlineTime"`
		IPWhite         bool           `json:"IpWhite"`
		IPWhitePass     string         `json:"IpWhitePass"`
		IPWhiteList     []string       `json:"IpWhiteList"`
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})
	var b bytes.Buffer
	id := 1
	for _, u := range users {
		if !u.Enabled || u.NPSVerifyKey == "" {
			continue
		}
		id++
		client := npsClient{
			Cnf:             map[string]any{"U": "", "P": "", "Compress": false, "Crypt": false},
			ID:              id,
			VerifyKey:       u.NPSVerifyKey,
			Remark:          u.Name,
			Status:          true,
			Flow:            map[string]int{"ExportFlow": 0, "InletFlow": 0, "FlowLimit": 0},
			Rate:            map[string]int{"NowRate": 17179869184},
			WebUserName:     u.Name,
			PortPool:        FormatPortRanges(u.PortPools),
			ConfigConnAllow: true,
			MaxTunnelNum:    u.MaxPorts,
			BlackIPList:     []string{},
			CreateTime:      u.CreatedAt.Format("2006-01-02 15:04:05"),
			IPWhiteList:     []string{},
		}
		record, err := json.Marshal(client)
		if err != nil {
			return nil, err
		}
		b.Write(record)
		b.WriteString("\n*#*\n")
	}
	return b.Bytes(), nil
}

func quoteList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	return strings.Join(quoted, ", ")
}
