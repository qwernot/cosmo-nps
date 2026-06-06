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
