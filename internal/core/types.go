package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	EngineFRP = "frp"
	EngineNPS = "nps"

	RoleAdmin = "admin"
	RoleUser  = "user"
)

type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func (r PortRange) Contains(port int) bool {
	return port >= r.Start && port <= r.End
}

func (r PortRange) String() string {
	if r.Start == r.End {
		return strconv.Itoa(r.Start)
	}
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

func ParsePortRanges(input string) ([]PortRange, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	parts := strings.Split(input, ",")
	ranges := make([]PortRange, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := parsePort(bounds[0])
			if err != nil {
				return nil, err
			}
			end, err := parsePort(bounds[1])
			if err != nil {
				return nil, err
			}
			if start > end {
				return nil, fmt.Errorf("invalid port range %q", part)
			}
			ranges = append(ranges, PortRange{Start: start, End: end})
			continue
		}
		port, err := parsePort(part)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, PortRange{Start: port, End: port})
	}
	return ranges, nil
}

func FormatPortRanges(ranges []PortRange) string {
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, ",")
}

func parsePort(input string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", input)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d out of range", port)
	}
	return port, nil
}

type User struct {
	Name         string      `json:"name"`
	Password     string      `json:"password,omitempty"`
	PasswordHash string      `json:"passwordHash,omitempty"`
	Role         string      `json:"role"`
	Enabled      bool        `json:"enabled"`
	PortPools    []PortRange `json:"portPools,omitempty"`
	MaxPorts     int         `json:"maxPorts,omitempty"`
	FRPToken     string      `json:"frpToken,omitempty"`
	NPSVerifyKey string      `json:"npsVerifyKey,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

type PublicUser struct {
	Name            string      `json:"name"`
	Role            string      `json:"role"`
	Enabled         bool        `json:"enabled"`
	PortPools       []PortRange `json:"portPools,omitempty"`
	MaxPorts        int         `json:"maxPorts,omitempty"`
	HasPassword     bool        `json:"hasPassword"`
	HasFRPToken     bool        `json:"hasFrpToken"`
	HasNPSVerifyKey bool        `json:"hasNpsVerifyKey"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

type Tunnel struct {
	ID         string    `json:"id"`
	UserName   string    `json:"userName"`
	Engine     string    `json:"engine"`
	Mode       string    `json:"mode"`
	RemotePort int       `json:"remotePort,omitempty"`
	LocalIP    string    `json:"localIp,omitempty"`
	LocalPort  int       `json:"localPort,omitempty"`
	Domains    []string  `json:"domains,omitempty"`
	Remark     string    `json:"remark,omitempty"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Database struct {
	Users   map[string]*User   `json:"users"`
	Tunnels map[string]*Tunnel `json:"tunnels"`
}

func Public(u *User) PublicUser {
	return PublicUser{
		Name:            u.Name,
		Role:            u.Role,
		Enabled:         u.Enabled,
		PortPools:       append([]PortRange(nil), u.PortPools...),
		MaxPorts:        u.MaxPorts,
		HasPassword:     u.Password != "" || u.PasswordHash != "",
		HasFRPToken:     u.FRPToken != "",
		HasNPSVerifyKey: u.NPSVerifyKey != "",
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}
