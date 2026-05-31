# Tunnel Control

Tunnel Control 是一个单容器穿透服务后台，把 `frp` 和 `nps` 的服务端能力嵌入到同一个 `tunnel-control` 进程里。

目标是只保留一个控制后台：

- 一个后台登录入口
- 一套管理员和普通用户
- 一套用户端口池和最大端口数限制
- 一个地方创建 FRP/NPS 隧道
- 用户只能看到自己的信息、端口池、隧道和客户端配置
- FRP/NPS 原生后台默认不对外开放

## 镜像

```bash
docker pull darkver8/tunnel-control:latest
```

镜像内只有一个主进程：

```text
tunnel-control
```

它会在进程内启动：

- 统一 Web 后台
- 嵌入式 frps
- 嵌入式 nps bridge
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
8088/tcp   统一后台
17000/tcp  FRP 客户端接入端口
18024/tcp  NPS bridge
18025/tcp  NPS TLS bridge
9080/tcp   NPS HTTP proxy
9443/tcp   NPS HTTPS proxy
```

FRP dashboard 和 NPS dashboard 默认关闭：

```text
FRP_DASHBOARD_PORT=0
NPS_WEB_PORT=0
```

## 使用流程

1. 登录统一后台。
2. 管理员创建用户，给用户分配固定端口池，例如 `12000-12010`。
3. 管理员或用户创建隧道，远程端口必须在该用户端口池内。
4. 在“配置”页选择用户，复制 `frpc.toml` 或 `npc` 命令。
5. 用户在自己的客户端机器上运行 `frpc` 或 `npc`。

保存用户时，后台会立即同步到 FRP 和 NPS 运行时，不需要手动同步。

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
name = "alice-frp-tcp-12001"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 12001
```

NPS 用户配置在后台“配置”页生成，格式类似：

```bash
./npc -server=SERVER_IP:18024 -vkey=用户自己的 verify key
```

## 数据目录

容器数据保存在 `/app/data`，建议挂载到宿主机：

```text
data/
  control/tunnel-control.json   # 统一后台数据库
  frp/frps-users.json           # FRP userStore 同步文件
  nps/conf/                     # NPS 运行配置和数据
  export/                       # 导出的客户端配置
```

## 常用命令

```bash
docker compose ps
docker compose logs -f tunnel-stack
docker compose restart tunnel-stack
docker compose down
```

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
