# Cosmo NPS 总控 Docker 部署

这个目录用于部署总控后台。总控只负责 Web 后台、用户、节点、隧道和配置下发，不启动 NPS，也不监听业务流量端口。

## 启动

```bash
cd deploy/docker
cp .env.example .env
nano .env
docker compose up -d
```

至少需要修改：

```text
PUBLIC_ADDR=总控服务器 IP 或域名
ADMIN_PASSWORD=强密码
```

访问：

```text
http://总控IP:8088
```

## 镜像

```text
darkver8/cosmo-nps:latest          总控后台
darkver8/cosmo-nps-node:latest     节点服务
darkver8/cosmo-nps-client:latest   用户客户端
```

三个镜像已经拆分构建，不再是同一个镜像打三个标签。

## 常用命令

```bash
docker compose ps
docker compose logs -f tunnel-stack
docker compose restart tunnel-stack
docker compose down
```

## 健康检查

```bash
curl http://127.0.0.1:8088/healthz
```

## 升级

```bash
bash upgrade.sh
```

`upgrade.sh` 会备份现有数据、拉取 `darkver8/cosmo-nps:latest`、重启容器并检查健康状态。
