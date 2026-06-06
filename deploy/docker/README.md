# 总控 Docker 部署

这个目录只用于部署总控后台。

总控不会启动 NPS，也不会监听 NPS 业务端口。它只负责：

- 登录和权限
- 用户、端口池、域名池
- 节点管理
- NPS 隧道配置
- 向节点 `tunnel-agent` 下发配置
- 查看日志、节点状态和可用性检测结果

## 启动

```bash
cd deploy/docker
cp .env.example .env
nano .env
docker compose up -d
```

至少修改：

```text
PUBLIC_ADDR=总控服务器 IP 或域名
ADMIN_PASSWORD=强密码
```

访问：

```text
http://总控服务器IP:8088
```

## 总控端口

```text
8088/tcp  Web 后台
```

NPS 业务端口由节点服务提供，不在总控容器里监听。

## 节点

节点使用 `deploy/agent/compose.yml`，启动入口是：

```text
/usr/local/bin/tunnel-agent
```

节点服务器需要放行：

```text
18089/tcp  节点配置接口，建议只允许总控访问
18024/tcp  NPS bridge
18025/tcp  NPS TLS bridge
9080/tcp   NPS HTTP proxy
9443/tcp   NPS HTTPS proxy
```
