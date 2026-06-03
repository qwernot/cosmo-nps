# Tunnel Control

Tunnel Control 是一个单容器穿透服务后台，把 `frp` 和 `nps` 的服务端能力嵌入到同一个 `tunnel-control` 进程里。

目标是只保留一个控制后台：

- 一个 Web 登录入口
- 总管理员和普通用户
- 统一的用户端口池、域名池和最大隧道数限制
- 一个地方创建 FRP/NPS 隧道
- 普通用户只能看到自己的资源池、隧道和客户端配置
- FRP/NPS 原生后台默认不对外开放

## 本次更新

- 后台前端恢复正常中文显示，并补上“引擎”页面。
- 引擎页面显示单容器内置 FRP/NPS 状态、端口和运行模式。
- HTTP/HTTPS 隧道改为域名池模式，TCP/UDP/SOCKS5 继续使用端口池。
- FRP 服务端增加域名池兜底校验，用户私自修改 `frpc.toml` 也不能越权绑定域名。
- 概览页增加监听自检和用户资源使用情况，方便判断 FRP/NPS 是否真的在监听。
- 新增“日志”页面，总管理员可以在统一后台查看、搜索、刷新和清空最近运行日志。
- Docker 部署目录增加 `upgrade.sh`，升级前会备份 `data` 并做健康检查。
- Docker 镜像已在 `192.168.6.64` 上重新部署测试，并推送到 Docker Hub。

## 镜像

```bash
docker pull darkver8/tunnel-control:latest
```

镜像内只有一个主进程：

```text
tunnel-control
```

这个主进程会同时启动：

- 统一 Web 后台
- 内置 FRP 服务端
- 内置 NPS bridge
- NPS HTTP/HTTPS proxy

## 快速部署

Linux 服务器推荐使用 host 网络，因为 FRP/NPS 需要按用户动态监听端口。

```bash
mkdir -p tunnel-control
cd tunnel-control
cat > compose.yml <<'EOF'
services:
  tunnel-stack:
    image: darkver8/tunnel-control:latest
    container_name: tunnel-stack
    restart: unless-stopped
    network_mode: host
    environment:
      PUBLIC_ADDR: 你的服务器IP或域名
      ADMIN_USER: admin
      ADMIN_PASSWORD: 请改成强密码
      FRP_BIND_PORT: 17000
      FRP_HTTP_PORT: 9081
      FRP_HTTPS_PORT: 9444
      NPS_BRIDGE_PORT: 18024
      NPS_TLS_BRIDGE_PORT: 18025
      NPS_HTTP_PORT: 9080
      NPS_HTTPS_PORT: 9443
      USER_PORT_RANGE: 10000-20000
    volumes:
      - ./data:/app/data
EOF

docker compose up -d
```

打开后台：

```text
http://服务器IP:8088
```

首次启动如果没有管理员，会用 `ADMIN_USER` / `ADMIN_PASSWORD` 创建管理员。

## 默认端口

```text
8088/tcp        统一后台
17000/tcp       FRP 客户端接入端口
18024/tcp       NPS bridge
18025/tcp       NPS TLS bridge
9080/tcp        NPS HTTP proxy
9443/tcp        NPS HTTPS proxy
9081/tcp        FRP HTTP vhost
9444/tcp        FRP HTTPS vhost
10000-20000/tcp 用户 TCP/SOCKS5 隧道端口范围
10000-20000/udp 用户 UDP 隧道端口范围
```

FRP dashboard 和 NPS dashboard 默认关闭：

```text
FRP_DASHBOARD_PORT=0
NPS_WEB_PORT=0
```

## 使用流程

1. 登录统一后台。
2. 管理员创建用户。
3. 给用户分配固定端口池，例如 `12000-12010`。
4. 如果要开 HTTP/HTTPS，给用户分配域名池，例如 `*.example.com,app.example.com`。
5. 管理员或用户创建隧道。
6. 在“配置”页选择用户，复制 `frpc.toml` 或 `npc` 命令。
7. 用户在自己的客户端机器上运行 `frpc` 或 `npc`。

保存用户或隧道后，后台会自动同步到嵌入式 FRP/NPS 运行时，不需要再去其他后台操作。

## 自检与资源

概览页会显示两类运行信息：

- 监听自检：管理员可看到后台、FRP、NPS 的关键 TCP 端口是否在容器内正常监听。
- 资源使用：显示每个用户的 TCP、UDP、域名和隧道额度使用量。

普通用户只能看到自己的资源使用情况；管理员可以看到全部用户和监听自检。

## 运行日志

总管理员可以在后台“日志”页面查看最近运行日志，用来排查后台请求、FRP/NPS 启动和隧道同步问题。

- 支持搜索日志关键字。
- 支持手动刷新。
- 支持清空当前内存日志。
- 日志只保存在当前进程内存中，容器重启后会重新开始记录。

## 隧道类型

TCP/UDP/SOCKS5 使用端口池：

- 必须填写远程端口
- 远程端口必须在用户端口池内
- 同一协议的同一个远程端口只能被一个隧道使用
- TCP/SOCKS5 和 UDP 可以使用同一个数字端口

HTTP/HTTPS 使用域名池：

- 必须填写域名
- 不需要填写远程端口
- 域名必须在用户域名池内
- 支持精确域名，例如 `app.example.com`
- 支持泛域名池，例如 `*.example.com`
- FRP 服务端也会校验域名池，用户私自修改 `frpc.toml` 不能越权绑定别人的域名

FRP 和 NPS 的 HTTP 入口端口不同：

```text
NPS HTTP:  http://域名:9080
NPS HTTPS: https://域名:9443
FRP HTTP:  http://域名:9081
FRP HTTPS: https://域名:9444
```

生产环境一般会把 80/443 通过安全组、端口转发或前置反向代理转发到对应入口端口。

## 客户端

FRP 用户配置在后台“配置”页生成，格式类似：

```toml
user = "alice"
serverAddr = "SERVER_IP"
serverPort = 17000
loginFailExit = true

[auth]
method = "token"
token = "用户自己的 token"

[[proxies]]
name = "alice-frp-http-xxxx"
type = "http"
customDomains = ["web.example.com"]
localIP = "127.0.0.1"
localPort = 8080
```

NPS 用户配置在后台“配置”页生成，格式类似：

```bash
./npc -server=SERVER_IP:18024 -vkey=用户自己的 verify key
```

NPS 的 HTTP/HTTPS 域名隧道会在服务端自动同步为 Host 记录，客户端仍然只需要运行同一个 `npc` 连接。

## 数据目录

容器数据保存在 `/app/data`，建议挂载到宿主机：

```text
data/
  control/tunnel-control.json   # 统一后台数据库
  frp/frps-users.json           # FRP userStore 同步文件
  nps/conf/                     # NPS 运行配置和数据
  export/                       # 导出的客户端配置
```

备份 `data` 目录即可保留后台用户、隧道和引擎数据。

## 常用命令

```bash
docker compose ps
docker compose logs -f tunnel-stack
docker compose restart tunnel-stack
docker compose down
```

升级镜像并自动备份、重启、健康检查：

```bash
cd deploy/docker
./upgrade.sh
```

脚本默认升级 `darkver8/tunnel-control:latest`，备份文件会写到 `deploy/docker/backups/`。

健康检查：

```bash
curl http://127.0.0.1:8088/healthz
```

## 本地源码构建

```bash
go test ./...
go build -o tunnel-control ./cmd/tunnel-control
```

Docker 构建：

```bash
docker build -t darkver8/tunnel-control:latest .
```
