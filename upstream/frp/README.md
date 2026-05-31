# frp Docker 部署说明

## 镜像

```bash
docker pull darkver8/frps:latest
docker pull darkver8/frpc:latest
```

## 准备目录

```bash
mkdir -p /opt/frp
```

## frps 配置

保存为 `/opt/frp/frps.toml`：

```toml
bindPort = 7000

[webServer]
addr = "0.0.0.0"
port = 7500

[userStore]
enable = true
path = "/data/frps-users.json"
adminUser = "admin"
adminPassword = "admin"
```

首次启动会自动创建总管理员。用户、密码、token、端口池会持久化到：

```text
/opt/frp/frps-users.json
```

## 启动 frps

推荐使用 host 网络，避免每个代理端口都要写 Docker 端口映射：

```bash
docker run -d \
  --name frps \
  --restart unless-stopped \
  --network host \
  -v /opt/frp/frps.toml:/etc/frp/frps.toml:ro \
  -v /opt/frp:/data \
  darkver8/frps:latest
```

镜像默认读取 `/etc/frp/frps.toml`。如果使用 Portainer、1Panel 等面板部署，命令或 Command 保持为空即可。

访问 Web 控制台：

```text
http://服务器IP:7500
```

默认账号：

```text
admin / admin
```

登录后建议立即修改管理员密码。

## 添加用户

在 Web 控制台的 `Users` 页面添加普通用户，并配置：

- 用户名
- Web 密码
- frpc token
- 端口池，例如 `10000-10010`
- 最大端口数

普通用户只能看到自己的客户端和代理。

## frpc Docker 部署

如果 frpc 部署在 Docker 中，并且需要访问宿主机本地服务，建议同样使用 host 网络。

示例配置 `/opt/frp/frpc.toml`：

```toml
user = "你的用户名"
serverAddr = "服务器IP"
serverPort = 7000
loginFailExit = true

[auth]
token = "用户的frpc token"

[[proxies]]
name = "web"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 10000
```

启动 frpc：

```bash
docker run -d \
  --name frpc \
  --restart unless-stopped \
  --network host \
  -v /opt/frp/frpc.toml:/etc/frp/frpc.toml:ro \
  darkver8/frpc:latest
```

镜像默认读取 `/etc/frp/frpc.toml`。如果使用 Portainer、1Panel 等面板部署，命令或 Command 保持为空即可。

## 常用命令

查看 frps 日志：

```bash
docker logs -f frps
```

查看 frpc 日志：

```bash
docker logs -f frpc
```

重启：

```bash
docker restart frps
docker restart frpc
```

停止并删除：

```bash
docker rm -f frps
docker rm -f frpc
```

更新镜像：

```bash
docker pull darkver8/frps:latest
docker pull darkver8/frpc:latest
docker rm -f frps frpc
# 然后重新执行启动命令
```
