# Docker 部署

这个目录提供 `darkver8/tunnel-control:latest` 的 Compose 部署示例。

当前部署是一个容器：

```text
tunnel-stack
```

容器内是一个主进程：

```text
tunnel-control
```

FRP 和 NPS 服务端逻辑嵌入在这个主进程里，不再单独启动 `frps` / `nps` 容器。

## 启动

```bash
cd deploy/docker
cp .env.example .env
nano .env
docker compose up -d
```

至少修改：

```text
PUBLIC_ADDR=服务器IP或域名
ADMIN_PASSWORD=强密码
```

访问：

```text
http://服务器IP:8088
```

## 端口

需要在服务器防火墙或安全组放行：

```text
8088/tcp   统一后台
17000/tcp  FRP 客户端接入端口
18024/tcp  NPS bridge
18025/tcp  NPS TLS bridge
9080/tcp   NPS HTTP proxy
9443/tcp   NPS HTTPS proxy
9081/tcp   FRP HTTP vhost
9444/tcp   FRP HTTPS vhost
10000-20000/tcp 用户隧道端口范围
10000-20000/udp 如果需要 UDP 隧道
```

FRP dashboard 和 NPS dashboard 默认关闭：

```text
FRP_DASHBOARD_PORT=0
NPS_WEB_PORT=0
```

## 使用

1. 登录统一后台。
2. 创建用户并分配固定端口池。
3. 如需 HTTP/HTTPS，给用户分配域名池。
4. 创建 FRP 或 NPS 隧道。
5. 在“配置”页复制该用户的 `frpc.toml` 或 `npc` 命令。
6. 用户启动客户端。

TCP/UDP/SOCKS5 隧道使用端口池。HTTP/HTTPS 隧道使用域名池，不需要远程端口。

## 数据

宿主机 `./data` 会挂载到容器 `/app/data`：

```text
data/control/tunnel-control.json
data/frp/frps-users.json
data/nps/conf/
data/export/
```

备份 `data` 目录即可保留后台用户、隧道和引擎数据。

## 命令

```bash
docker compose ps
docker compose logs -f tunnel-stack
docker compose restart tunnel-stack
docker compose down
```
