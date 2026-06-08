# Tunnel Control

Tunnel Control 是一个 NPS 多节点统一管理项目。当前新项目只做 NPS，不再使用 FRP 主流程。

## 三个镜像

```bash
docker pull darkver8/tunnel-all:latest
docker pull darkver8/tunnel-port:latest
docker pull darkver8/tunnel-client:latest
```

三个镜像的职责：

```text
darkver8/tunnel-all      总控后台，只负责 Web、用户、节点、隧道和配置下发
darkver8/tunnel-port     节点服务，运行 tunnel-agent，启动 NPS 并接收总控配置
darkver8/tunnel-client   用户客户端，运行 tunnel-client，只连总控并自动连接对应节点
```

冻结的旧镜像 `darkver8/tunnel-control` 不再用于这个新项目。

## 总控部署

总控只需要开放后台端口 `8088`。它不监听 NPS 业务端口。

`docker-compose.yml`：

```yaml
services:
  tunnel-control:
    image: darkver8/tunnel-all:latest
    container_name: tunnel-stack
    restart: unless-stopped
    network_mode: host
    environment:
      PUBLIC_ADDR: 192.168.6.64
      ADMIN_USER: admin
      ADMIN_PASSWORD: change-this-password
    volumes:
      - ./data:/app/data
```

启动：

```bash
docker compose up -d
```

访问：

```text
http://192.168.6.64:8088
```

首次启动时，如果没有可用管理员，会用 `ADMIN_USER` / `ADMIN_PASSWORD` 自动创建管理员。

## 节点部署

先在总控后台“节点”页面创建节点，例如：

```text
节点 ID: edge-8
节点名称: 8.162.0.198
公网地址: 8.162.0.198
端口池: 10000-20000
NPS: 启用
```

保存后复制节点 Token。

节点服务器上的 `docker-compose.yml`：

```yaml
services:
  tunnel-agent:
    image: darkver8/tunnel-port:latest
    container_name: tunnel-agent-edge8
    restart: unless-stopped
    network_mode: host
    entrypoint: ["/usr/local/bin/tunnel-agent"]
    environment:
      CONTROL_URL: http://192.168.6.64:8088
      NODE_ID: edge-8
      NODE_TOKEN: paste-node-token-here
      AGENT_API_ADDR: ":18089"
      NPS_BRIDGE_PORT: 18024
      NPS_TLS_BRIDGE_PORT: 18025
      NPS_HTTP_PORT: 9080
      NPS_HTTPS_PORT: 9443
    volumes:
      - ./data:/app/data
```

启动：

```bash
docker compose up -d
```

节点会启动 NPS，并定时从总控拉取配置。总控也可以主动推送配置到节点的 `18089` 接口。

## 用户客户端部署

用户侧运行 `tunnel-client`。用户只需要填写总控地址、后台用户名和后台密码，不需要知道具体应该连接哪个节点。

新版本重构并美化了用户客户端，统一采用 Go 编写的 Web GUI 桌面架构，服务内嵌极简的高级毛玻璃 Dashboard。支持 Windows 系统托盘、静默运行、开机自启动及系统服务安装。同时跨平台兼容 Linux。

### 1. Windows 客户端（GUI 桌面版）

提供完全的桌面客户端体验：
- **无窗口运行**：直接双击 `tunnel-client.exe` 启动，不会弹出任何命令行 CMD 黑框（使用 GUI 链接标志构建）。
- **系统托盘**：启动后缩入系统右下角托盘，内嵌精美 3D 发光图标。右键菜单支持：打开管理面板、设置开机自启动、退出。
- **开机自启动**：可在网页设置或右键托盘中一键开启，Windows 登录时在后台以 `-silent` 静默模式启动。
- **系统服务安装**：支持注册为原生 Windows 服务，并在后台持久运行。
  - 安装服务：`tunnel-client.exe -service-install`
  - 卸载服务：`tunnel-client.exe -service-uninstall`
- **Web 管理面板**：本地服务默认监听 `http://127.0.0.1:18090`，展示发光指示灯状态、连接设置、本地启用的穿透隧道详情及实时高亮着色日志。

**构建 Windows 版本**：
```bash
# 包含高清图标资源并配置无 console 窗口运行标志
go build -ldflags="-H windowsgui -s -w" -o tunnel-client.exe ./cmd/tunnel-client
```

### 2. Linux 客户端（守护进程与 Web GUI 版）

Linux 客户端经过重构适配，支持跨平台编译：
- **无需图形依赖**：在 Linux 下默认以 headless 守护进程配合 Web GUI 模式运行。可在 `CGO_ENABLED=0` 交叉编译，无需任何 GTK/C 编译器环境。
- **Web 控制台**：启动后通过 `xdg-open` 自动唤起系统浏览器并提供与 Windows 相同的毛玻璃 Web 仪表盘。
- **命令行模式**：直接传入参数进行传统无界面部署。

**构建 Linux 版本**：
```bash
# CGO_ENABLED=0 交叉编译 Linux headless 版本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tunnel-client-linux ./cmd/tunnel-client
```

### 3. 命令行静默启动模式（通用）

支持直接通过传参启动（不显示 GUI，直接连接）：
```bash
tunnel-client -server http://192.168.6.64:8088 -user dark -password change-this-password
```
不带参数启动时，会自动进入本地 Web 启动器服务（默认地址 `127.0.0.1:18090`）。

### Docker 客户端

用户客户端机器上的 `docker-compose.yml`：

```yaml
services:
  tunnel-client:
    image: darkver8/tunnel-client:latest
    container_name: tunnel-client
    restart: unless-stopped
    network_mode: host
    entrypoint: ["/usr/local/bin/tunnel-client"]
    environment:
      CONTROL_URL: http://192.168.6.64:8088
      TUNNEL_USER: dark
      TUNNEL_PASSWORD: change-this-password
```

启动：

```bash
docker compose up -d
```

客户端会登录总控，读取该用户所有启用的 NPS 隧道，然后自动连接对应节点的 NPS bridge。

## 端口放行

总控服务器：

```text
8088/tcp  Web 后台
```

节点服务器：

```text
18089/tcp       节点配置接口，建议只允许总控访问
18024/tcp       NPS bridge
18025/tcp       NPS TLS bridge
9080/tcp        NPS HTTP proxy
9443/tcp        NPS HTTPS proxy
10000-20000/tcp 用户 TCP/SOCKS5 隧道端口
10000-20000/udp 用户 UDP 隧道端口
```

## 使用流程

1. 登录总控后台。
2. 创建用户，并给用户分配固定端口池和域名池。
3. 创建节点，复制节点 Token。
4. 在节点服务器部署 `darkver8/tunnel-port`。
5. 在后台给用户创建 NPS 隧道，选择目标节点。
6. 用户侧运行 `darkver8/tunnel-client`，只连接总控即可。

## 常用命令

总控：

```bash
docker compose ps
docker compose logs -f tunnel-stack
docker compose restart tunnel-stack
docker compose down
```

节点：

```bash
docker compose ps
docker compose logs -f tunnel-agent
docker compose restart tunnel-agent
docker compose down
```

用户客户端：

```bash
docker compose ps
docker compose logs -f tunnel-client
docker compose restart tunnel-client
docker compose down
```

健康检查：

```bash
curl http://127.0.0.1:8088/healthz
```

## 本地构建

```bash
go test ./...
docker build -t darkver8/tunnel-all:latest .
docker tag darkver8/tunnel-all:latest darkver8/tunnel-port:latest
docker tag darkver8/tunnel-all:latest darkver8/tunnel-client:latest
```
