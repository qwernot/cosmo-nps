package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	EngineFRP = "frp"
	EngineNPS = "nps"

	RoleAdmin = "admin"
	RoleUser  = "user"

	DefaultNodeID = "local"
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

func ParseDomainPools(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	parts := strings.Split(input, ",")
	domains := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		domain, err := NormalizeDomain(part)
		if err != nil {
			return nil, err
		}
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	return domains, nil
}

func FormatDomainPools(domains []string) string {
	return strings.Join(domains, ",")
}

func NormalizeDomains(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		domain, err := NormalizeDomain(value)
		if err != nil {
			return nil, err
		}
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		out = append(out, domain)
	}
	return out, nil
}

func NormalizeDomain(input string) (string, error) {
	domain := strings.TrimSpace(strings.ToLower(input))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	if i := strings.IndexAny(domain, "/:"); i >= 0 {
		domain = domain[:i]
	}
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		return "", nil
	}
	if domain == "*" {
		return domain, nil
	}
	if strings.HasPrefix(domain, "*.") {
		base := strings.TrimPrefix(domain, "*.")
		if err := validateDomain(base); err != nil {
			return "", fmt.Errorf("invalid domain %q: %w", input, err)
		}
		return domain, nil
	}
	if strings.Contains(domain, "*") {
		return "", fmt.Errorf("invalid domain %q", input)
	}
	if err := validateDomain(domain); err != nil {
		return "", fmt.Errorf("invalid domain %q: %w", input, err)
	}
	return domain, nil
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

func validateDomain(domain string) error {
	if len(domain) > 253 {
		return fmt.Errorf("too long")
	}
	if ip := net.ParseIP(domain); ip != nil {
		return nil
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("must contain at least one dot")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("invalid label")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("label cannot start or end with hyphen")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return fmt.Errorf("contains unsupported character %q", r)
		}
	}
	return nil
}

type User struct {
	Name         string      `json:"name"`
	Password     string      `json:"password,omitempty"`
	PasswordHash string      `json:"passwordHash,omitempty"`
	Role         string      `json:"role"`
	Enabled      bool        `json:"enabled"`
	PortPools    []PortRange `json:"portPools,omitempty"`
	DomainPools  []string    `json:"domainPools,omitempty"`
	MaxPorts     int         `json:"maxPorts,omitempty"`
	FRPToken     string      `json:"frpToken,omitempty"`
	NPSVerifyKey string      `json:"npsVerifyKey,omitempty"`
	RateLimit    int         `json:"rateLimit,omitempty"`
	FlowLimit    int64       `json:"flowLimit,omitempty"`
	FlowUsed     int64       `json:"flowUsed,omitempty"`
	ExpiresAt    time.Time   `json:"expiresAt,omitempty"`
	ExpiredAt    time.Time   `json:"expiredAt,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

type PublicUser struct {
	Name            string      `json:"name"`
	Role            string      `json:"role"`
	Enabled         bool        `json:"enabled"`
	PortPools       []PortRange `json:"portPools,omitempty"`
	DomainPools     []string    `json:"domainPools,omitempty"`
	MaxPorts        int         `json:"maxPorts,omitempty"`
	HasPassword     bool        `json:"hasPassword"`
	HasFRPToken     bool        `json:"hasFrpToken"`
	HasNPSVerifyKey bool        `json:"hasNpsVerifyKey"`
	RateLimit       int         `json:"rateLimit,omitempty"`
	FlowLimit       int64       `json:"flowLimit,omitempty"`
	FlowUsed        int64       `json:"flowUsed,omitempty"`
	InletSpeed      int64       `json:"inletSpeed,omitempty"`
	ExportSpeed     int64       `json:"exportSpeed,omitempty"`
	ExpiresAt       time.Time   `json:"expiresAt,omitempty"`
	ExpiredAt       time.Time   `json:"expiredAt,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

type Node struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Token       string        `json:"token,omitempty"`
	PublicAddr  string        `json:"publicAddr,omitempty"`
	Enabled     bool          `json:"enabled"`
	FRPEnabled  bool          `json:"frpEnabled"`
	NPSEnabled  bool          `json:"npsEnabled"`
	PortPools   []PortRange   `json:"portPools,omitempty"`
	DomainPools []string      `json:"domainPools,omitempty"`
	Runtime     RuntimeConfig `json:"runtime,omitempty"`
	Status      NodeStatus    `json:"status,omitzero"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

type NodeStatus struct {
	Online     bool      `json:"online"`
	LastSeenAt time.Time `json:"lastSeenAt,omitzero"`
	LastSyncAt time.Time `json:"lastSyncAt,omitzero"`
	LastError  string    `json:"lastError,omitempty"`
}

func (s NodeStatus) IsZero() bool {
	return !s.Online && s.LastSeenAt.IsZero() && s.LastSyncAt.IsZero() && s.LastError == ""
}

type Tunnel struct {
	ID         string    `json:"id"`
	UserName   string    `json:"userName"`
	NodeID     string    `json:"nodeId,omitempty"`
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
	Nodes   map[string]*Node   `json:"nodes,omitempty"`
	Tunnels map[string]*Tunnel `json:"tunnels"`
}

func Public(u *User) PublicUser {
	return PublicUser{
		Name:            u.Name,
		Role:            u.Role,
		Enabled:         u.Enabled,
		PortPools:       append([]PortRange(nil), u.PortPools...),
		DomainPools:     append([]string(nil), u.DomainPools...),
		MaxPorts:        u.MaxPorts,
		HasPassword:     u.Password != "" || u.PasswordHash != "",
		HasFRPToken:     u.FRPToken != "",
		HasNPSVerifyKey: u.NPSVerifyKey != "",
		RateLimit:       u.RateLimit,
		FlowLimit:       u.FlowLimit,
		FlowUsed:        u.FlowUsed,
		ExpiresAt:       u.ExpiresAt,
		ExpiredAt:       u.ExpiredAt,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

type TunnelTraffic struct {
	TunnelID   string `json:"tunnelId"`
	InletFlow  int64  `json:"inletFlow"`
	ExportFlow int64  `json:"exportFlow"`
}

type UserTraffic struct {
	UserName   string `json:"userName"`
	InletFlow  int64  `json:"inletFlow"`
	ExportFlow int64  `json:"exportFlow"`
}

type TrafficReport struct {
	NodeID  string          `json:"nodeId"`
	Tunnels []TunnelTraffic `json:"tunnels"`
	Users   []UserTraffic   `json:"users"`
}
