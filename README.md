# Cosmo NPS

Cosmo NPS 是一个基于 NPS 的多节点统一管理项目。总控只负责 Web 后台、用户、节点、隧道和配置下发；节点负责运行 NPS；客户端只连接总控并自动连接用户被分配的节点。

当前项目只做 NPS 主流程，不再把 FRP 作为核心路径。

## 镜像

```bash
docker pull darkver8/cosmo-nps:latest
docker pull darkver8/cosmo-nps-node:latest
docker pull darkver8/cosmo-nps-client:latest
```

```text
darkver8/cosmo-nps          总控后台
darkver8/cosmo-nps-node     节点服务，运行 tunnel-agent 和 NPS
darkver8/cosmo-nps-client   用户客户端，运行 Cosmo NPS Client
```

旧镜像 `darkver8/tunnel-control` 属于冻结旧项目；`darkver8/tunnel-all`、`darkver8/tunnel-port`、`darkver8/tunnel-client` 后续只建议用于兼容历史部署。

## 总控部署

总控只需要开放后台端口 `8088`，不监听 NPS 业务端口。

```bash
git clone https://github.com/qwernot/cosmo-nps.git
cd cosmo-nps
sudo bash deploy/quick-deploy.sh
```

静默部署示例：

```bash
sudo PUBLIC_ADDR=192.168.6.64 \
  ADMIN_USER=admin \
  ADMIN_PASSWORD='change-this-password' \
  NONINTERACTIVE=1 \
  bash deploy/quick-deploy.sh
```

Compose 示例：

```yaml
services:
  cosmo-nps:
    image: darkver8/cosmo-nps:latest
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

访问：

```text
http://192.168.6.64:8088
```

如果已有 `data` 数据目录，`ADMIN_USER` / `ADMIN_PASSWORD` 只用于首次初始化，不会重置已有管理员密码。

## 节点部署

先在总控后台创建节点，复制节点 Token，然后在节点服务器部署：

```yaml
services:
  cosmo-nps-node:
    image: darkver8/cosmo-nps-node:latest
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

节点会启动 NPS，并定时从总控拉取配置。总控也可以主动推送配置到节点的 `18089` 接口。

## 客户端

用户侧运行 Cosmo NPS Client。用户只需要填写总控地址、后台用户名和后台密码，不需要知道具体节点信息。

Windows 客户端支持：

- WPF 桌面 GUI
- 托盘常驻
- 开机自启动
- 一键重连
- 当前连接节点显示
- 本地运行日志

构建 Windows 客户端：

```bash
dotnet build ./cmd/tunnel-client-gui/TunnelClientGui.csproj -c Release
go build -ldflags="-H windowsgui -s -w" -o tunnel-client.exe ./cmd/tunnel-client
```

命令行运行：

```bash
tunnel-client -server http://192.168.6.64:8088 -user dark -password change-this-password
```

Docker 客户端：

```yaml
services:
  cosmo-nps-client:
    image: darkver8/cosmo-nps-client:latest
    container_name: tunnel-client
    restart: unless-stopped
    network_mode: host
    entrypoint: ["/usr/local/bin/tunnel-client"]
    environment:
      CONTROL_URL: http://192.168.6.64:8088
      TUNNEL_USER: dark
      TUNNEL_PASSWORD: change-this-password
```

## 端口

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

客户端：

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

## 本地构建与推送

```bash
go test ./...
bash build-and-push.sh
```

带版本号：

```bash
bash build-and-push.sh v1.0.0
```
