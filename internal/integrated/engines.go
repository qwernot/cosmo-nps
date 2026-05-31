package integrated

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	npsbridge "ehang.io/nps/bridge"
	npscommon "ehang.io/nps/lib/common"
	npsfile "ehang.io/nps/lib/file"
	npsserver "ehang.io/nps/server"
	npsconnection "ehang.io/nps/server/connection"
	npsrouters "ehang.io/nps/web/routers"
	"github.com/astaxie/beego"
	frptypes "github.com/fatedier/frp/pkg/config/types"
	frpconfig "github.com/fatedier/frp/pkg/config/v1"
	frpserver "github.com/fatedier/frp/server"
	frpuserstore "github.com/fatedier/frp/server/userstore"

	"qwernot/tunnel-control/internal/core"
)

var (
	frpMu  sync.RWMutex
	frpSvc *frpserver.Service
)

type FRPOptions struct {
	BindPort  int
	HTTPPort  int
	HTTPSPort int
	WebPort   int
	UserFile  string
	Admin     string
	Password  string
}

func RunFRP(ctx context.Context, opt FRPOptions) error {
	cfg := &frpconfig.ServerConfig{
		BindPort:       opt.BindPort,
		VhostHTTPPort:  opt.HTTPPort,
		VhostHTTPSPort: opt.HTTPSPort,
		UserStore: frpconfig.UserStoreConfig{
			Enable:        true,
			Path:          opt.UserFile,
			AdminUser:     opt.Admin,
			AdminPassword: opt.Password,
		},
		WebServer: frpconfig.WebServerConfig{
			Addr:     "0.0.0.0",
			Port:     opt.WebPort,
			User:     opt.Admin,
			Password: opt.Password,
		},
	}
	if err := cfg.Complete(); err != nil {
		return err
	}
	svc, err := frpserver.NewService(cfg)
	if err != nil {
		return err
	}
	frpMu.Lock()
	frpSvc = svc
	frpMu.Unlock()
	defer func() {
		frpMu.Lock()
		if frpSvc == svc {
			frpSvc = nil
		}
		frpMu.Unlock()
	}()
	svc.Run(ctx)
	return nil
}

func SyncFRPState(users []core.User) error {
	frpMu.RLock()
	svc := frpSvc
	frpMu.RUnlock()
	if svc == nil {
		return nil
	}

	desired := map[string]bool{}
	for _, user := range users {
		if !user.Enabled || user.FRPToken == "" {
			continue
		}
		desired[user.Name] = true
		if err := svc.UpsertUserStoreUser(frpuserstore.User{
			Name:       user.Name,
			Token:      user.FRPToken,
			Role:       user.Role,
			Enabled:    true,
			AllowPorts: toFRPPortRanges(user.PortPools),
			Domains:    append([]string(nil), user.DomainPools...),
			MaxPorts:   user.MaxPorts,
		}); err != nil {
			return err
		}
	}

	existing, err := svc.ListUserStoreUsers()
	if err != nil {
		return err
	}
	for _, user := range existing {
		if desired[user.Name] || user.Role == frpuserstore.RoleAdmin {
			continue
		}
		if err := svc.DeleteUserStoreUser(user.Name); err != nil {
			return err
		}
	}
	return nil
}

func toFRPPortRanges(ranges []core.PortRange) []frptypes.PortsRange {
	out := make([]frptypes.PortsRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			out = append(out, frptypes.PortsRange{Single: r.Start})
			continue
		}
		out = append(out, frptypes.PortsRange{Start: r.Start, End: r.End})
	}
	return out
}

type NPSOptions struct {
	WorkDir    string
	BridgePort int
}

func RunNPS(ctx context.Context, opt NPSOptions) error {
	if opt.WorkDir == "" {
		return fmt.Errorf("nps work dir is required")
	}
	npscommon.ConfPath = opt.WorkDir
	if err := beego.LoadAppConfig("ini", filepath.Join(opt.WorkDir, "conf", "nps.conf")); err != nil {
		return err
	}
	npsrouters.Init()
	npsfile.GetDb()
	npsconnection.InitConnectionService()
	npsbridge.ServerTlsEnable = beego.AppConfig.DefaultBool("tls_enable", false)
	cnf := &npsfile.Tunnel{
		Id:     1,
		Port:   0,
		Mode:   "webServer",
		Status: true,
	}
	npsserver.StartNewServer(opt.BridgePort, cnf, "tcp", 60)
	<-ctx.Done()
	return ctx.Err()
}

func SyncNPSState(users []core.User, tunnels []core.Tunnel) error {
	if npsfile.Db == nil {
		return nil
	}
	db := npsfile.GetDb()
	clients := map[string]*npsfile.Client{}
	desiredUsers := map[string]bool{}
	for _, user := range users {
		if !user.Enabled || user.NPSVerifyKey == "" {
			continue
		}
		desiredUsers[user.Name] = true
		client := findNPSClient(user.Name)
		if client == nil {
			client = npsfile.NewClient(user.NPSVerifyKey, false, false)
		}
		client.VerifyKey = user.NPSVerifyKey
		client.Remark = user.Name
		client.Status = true
		client.WebUserName = user.Name
		client.PortPool = core.FormatPortRanges(user.PortPools)
		client.ConfigConnAllow = true
		client.MaxTunnelNum = user.MaxPorts
		if err := db.NewClient(client); err != nil {
			return err
		}
		clients[user.Name] = client
	}

	desiredTasks := map[string]bool{}
	desiredHosts := map[string]bool{}
	for _, tunnel := range tunnels {
		if tunnel.Engine != core.EngineNPS || !tunnel.Enabled {
			continue
		}
		client := clients[tunnel.UserName]
		if client == nil {
			continue
		}
		if tunnel.Mode == "http" || tunnel.Mode == "https" {
			for _, domain := range tunnel.Domains {
				remark := managedNPSHostRemark(tunnel.ID, domain)
				desiredHosts[remark] = true
				host := findNPSHost(remark)
				if host == nil {
					host = &npsfile.Host{Id: int(db.JsonDb.GetHostId())}
				}
				host.Host = domain
				host.Target = &npsfile.Target{TargetStr: fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)}
				host.HeaderChange = ""
				host.HostChange = ""
				host.Remark = remark
				host.Location = "/"
				host.Flow = &npsfile.Flow{}
				host.Scheme = tunnel.Mode
				host.Client = client
				host.IsClose = false
				host.AutoHttps = false
				if existing := findNPSHostByDomain(domain, tunnel.Mode, remark); existing != nil {
					return fmt.Errorf("nps host %s/%s already exists in %s", tunnel.Mode, domain, existing.Remark)
				}
				if err := upsertNPSHost(db, host); err != nil {
					return err
				}
			}
			continue
		}
		remark := managedNPSRemark(tunnel.ID)
		desiredTasks[remark] = true
		task := findNPSTask(remark)
		if task == nil {
			task = &npsfile.Tunnel{Id: int(db.JsonDb.GetTaskId())}
		}
		task.Port = tunnel.RemotePort
		task.Mode = tunnel.Mode
		task.Status = true
		task.Client = client
		task.Remark = remark
		task.Target = &npsfile.Target{TargetStr: fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)}
		task.Flow = &npsfile.Flow{}
		if task.Mode == "secret" || task.Mode == "p2p" {
			task.Password = tunnel.ID
		}
		if err := db.UpdateTask(task); err != nil {
			return err
		}
		if npsserver.Bridge != nil {
			if _, ok := npsserver.RunList.Load(task.Id); ok {
				_ = npsserver.StopServer(task.Id)
			}
			if err := npsserver.AddTask(task); err != nil {
				return err
			}
		}
	}

	db.JsonDb.Tasks.Range(func(key, value any) bool {
		task, ok := value.(*npsfile.Tunnel)
		if !ok || !strings.HasPrefix(task.Remark, "tc:") || desiredTasks[task.Remark] {
			return true
		}
		if npsserver.Bridge != nil {
			if _, ok := npsserver.RunList.Load(task.Id); ok {
				_ = npsserver.StopServer(task.Id)
			}
		}
		_ = db.DelTask(task.Id)
		return true
	})

	db.JsonDb.Hosts.Range(func(key, value any) bool {
		host, ok := value.(*npsfile.Host)
		if !ok || !strings.HasPrefix(host.Remark, "tc:") || desiredHosts[host.Remark] {
			return true
		}
		_ = db.DelHost(host.Id)
		return true
	})

	db.JsonDb.Clients.Range(func(_, value any) bool {
		client, ok := value.(*npsfile.Client)
		if !ok {
			return true
		}
		name := client.WebUserName
		if name == "" {
			name = client.Remark
		}
		if name == "" || desiredUsers[name] {
			return true
		}
		if npsserver.Bridge != nil {
			npsserver.DelClientConnect(client.Id)
		}
		npsserver.DelTunnelAndHostByClientId(client.Id, false)
		_ = db.DelClient(client.Id)
		return true
	})
	return nil
}

func findNPSClient(name string) *npsfile.Client {
	var client *npsfile.Client
	npsfile.GetDb().JsonDb.Clients.Range(func(_, value any) bool {
		c, ok := value.(*npsfile.Client)
		if !ok {
			return true
		}
		if c.WebUserName == name || c.Remark == name {
			client = c
			return false
		}
		return true
	})
	return client
}

func findNPSTask(remark string) *npsfile.Tunnel {
	var task *npsfile.Tunnel
	npsfile.GetDb().JsonDb.Tasks.Range(func(_, value any) bool {
		t, ok := value.(*npsfile.Tunnel)
		if !ok {
			return true
		}
		if t.Remark == remark {
			task = t
			return false
		}
		return true
	})
	return task
}

func findNPSHost(remark string) *npsfile.Host {
	var host *npsfile.Host
	npsfile.GetDb().JsonDb.Hosts.Range(func(_, value any) bool {
		h, ok := value.(*npsfile.Host)
		if !ok {
			return true
		}
		if h.Remark == remark {
			host = h
			return false
		}
		return true
	})
	return host
}

func findNPSHostByDomain(domain, scheme, exceptRemark string) *npsfile.Host {
	var host *npsfile.Host
	npsfile.GetDb().JsonDb.Hosts.Range(func(_, value any) bool {
		h, ok := value.(*npsfile.Host)
		if !ok || h.Remark == exceptRemark {
			return true
		}
		if h.Host == domain && h.Location == "/" && (h.Scheme == scheme || h.Scheme == "all") {
			host = h
			return false
		}
		return true
	})
	return host
}

func upsertNPSHost(db *npsfile.DbUtils, host *npsfile.Host) error {
	if host.Location == "" {
		host.Location = "/"
	}
	if host.Flow == nil {
		host.Flow = &npsfile.Flow{}
	}
	db.JsonDb.Hosts.Store(host.Id, host)
	db.JsonDb.StoreHostToJsonFile()
	return nil
}

func managedNPSRemark(id string) string {
	return "tc:" + id
}

func managedNPSHostRemark(id, domain string) string {
	return "tc:" + id + ":" + domain
}
