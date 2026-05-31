package userstore

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fatedier/frp/pkg/auth"
	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
)

const RoleAdmin = "admin"

type User struct {
	Name        string             `json:"name"`
	Password    string             `json:"password,omitempty"`
	Token       string             `json:"token,omitempty"`
	Role        string             `json:"role,omitempty"`
	Enabled     bool               `json:"enabled"`
	AllowPorts  []types.PortsRange `json:"allowPorts,omitempty"`
	MaxPorts    int                `json:"maxPorts,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
	LastLoginAt time.Time          `json:"lastLoginAt,omitempty"`
}

type PublicUser struct {
	Name        string             `json:"name"`
	Role        string             `json:"role,omitempty"`
	Enabled     bool               `json:"enabled"`
	AllowPorts  []types.PortsRange `json:"allowPorts,omitempty"`
	MaxPorts    int                `json:"maxPorts,omitempty"`
	HasPassword bool               `json:"hasPassword"`
	HasToken    bool               `json:"hasToken"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
	LastLoginAt time.Time          `json:"lastLoginAt,omitempty"`
}

type ContextUser struct {
	Name string
	Role string
}

type contextKey struct{}

func FromContext(ctx context.Context) (ContextUser, bool) {
	u, ok := ctx.Value(contextKey{}).(ContextUser)
	return u, ok
}

type Store struct {
	path string
	mu   sync.RWMutex
	data map[string]*User
}

func NewStore(cfg v1.UserStoreConfig) (*Store, error) {
	s := &Store{
		path: cfg.Path,
		data: make(map[string]*User),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.data) == 0 {
		now := time.Now()
		s.data[cfg.AdminUser] = &User{
			Name:      cfg.AdminUser,
			Password:  cfg.AdminPassword,
			Role:      RoleAdmin,
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) List() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicUser, 0, len(s.data))
	for _, u := range s.data {
		out = append(out, toPublicUser(u))
	}
	slices.SortFunc(out, func(a, b PublicUser) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (s *Store) Get(name string) (PublicUser, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data[name]
	if !ok {
		return PublicUser{}, false
	}
	return toPublicUser(u), true
}

func (s *Store) Upsert(in User) (PublicUser, error) {
	if in.Name == "" {
		return PublicUser{}, fmt.Errorf("user name is required")
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Role != "user" && in.Role != RoleAdmin {
		return PublicUser{}, fmt.Errorf("role must be user or admin")
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.data[in.Name]
	if !ok {
		u = &User{Name: in.Name, CreatedAt: now}
		s.data[in.Name] = u
	}
	u.Role = in.Role
	u.Enabled = in.Enabled
	u.AllowPorts = slices.Clone(in.AllowPorts)
	u.MaxPorts = in.MaxPorts
	if in.Password != "" {
		u.Password = in.Password
	}
	if in.Token != "" {
		u.Token = in.Token
	}
	u.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return PublicUser{}, err
	}
	return toPublicUser(u), nil
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.data[name]
	if !ok {
		return nil
	}
	if u.Role == RoleAdmin {
		for _, other := range s.data {
			if other.Name != name && other.Role == RoleAdmin && other.Enabled {
				delete(s.data, name)
				return s.saveLocked()
			}
		}
		return fmt.Errorf("cannot delete the last enabled admin")
	}
	delete(s.data, name)
	return s.saveLocked()
}

func (s *Store) VerifyWebLogin(name, password string) (PublicUser, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.data[name]
	if !ok || !u.Enabled || u.Password == "" || !constantTimeEqual(u.Password, password) {
		return PublicUser{}, false
	}
	u.LastLoginAt = time.Now()
	_ = s.saveLocked()
	return toPublicUser(u), true
}

func (s *Store) VerifyClientToken(name, token string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data[name]
	if !ok || !u.Enabled || u.Token == "" || !constantTimeEqual(u.Token, token) {
		return nil, false
	}
	cp := cloneUser(u)
	return cp, true
}

func (s *Store) ClientAuth(login *msg.Login, scopes []v1.AuthScope) (auth.Verifier, []byte, error) {
	if login.User == "" {
		return nil, nil, fmt.Errorf("user is required")
	}
	s.mu.RLock()
	u, ok := s.data[login.User]
	if !ok {
		s.mu.RUnlock()
		return nil, nil, fmt.Errorf("user [%s] not found", login.User)
	}
	u = cloneUser(u)
	s.mu.RUnlock()
	if !u.Enabled {
		return nil, nil, fmt.Errorf("user [%s] is disabled", login.User)
	}
	if u.Token == "" {
		return nil, nil, fmt.Errorf("user [%s] has no frpc token", login.User)
	}
	verifier := auth.NewTokenAuth(scopes, u.Token)
	if err := verifier.VerifyLogin(login); err != nil {
		return nil, nil, err
	}
	return verifier, []byte(u.Token), nil
}

func (s *Store) UserPorts(name string) []types.PortsRange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data[name]
	if !ok || !u.Enabled {
		return nil
	}
	return slices.Clone(u.AllowPorts)
}

func (s *Store) UserMaxPorts(name string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data[name]
	if !ok || !u.Enabled {
		return 0
	}
	return u.MaxPorts
}

func (s *Store) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		user, password, ok := req.BasicAuth()
		if !ok {
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		u, ok := s.VerifyWebLogin(user, password)
		if !ok {
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(req.Context(), contextKey{}, ContextUser{
			Name: u.Name,
			Role: u.Role,
		})
		next.ServeHTTP(rw, req.WithContext(ctx))
	})
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var users []*User
	if err := json.Unmarshal(b, &users); err != nil {
		return err
	}
	for _, u := range users {
		if u.Name == "" {
			continue
		}
		s.data[u.Name] = cloneUser(u)
	}
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	users := make([]*User, 0, len(s.data))
	for _, u := range s.data {
		users = append(users, cloneUser(u))
	}
	slices.SortFunc(users, func(a, b *User) int {
		return strings.Compare(a.Name, b.Name)
	})
	b, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func toPublicUser(u *User) PublicUser {
	return PublicUser{
		Name:        u.Name,
		Role:        u.Role,
		Enabled:     u.Enabled,
		AllowPorts:  slices.Clone(u.AllowPorts),
		MaxPorts:    u.MaxPorts,
		HasPassword: u.Password != "",
		HasToken:    u.Token != "",
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

func cloneUser(u *User) *User {
	out := *u
	out.AllowPorts = slices.Clone(u.AllowPorts)
	return &out
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
