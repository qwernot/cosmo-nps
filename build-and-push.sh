#!/bin/bash
# tunnel-all 镜像构建并推送脚本
# 用法: bash build-and-push.sh [版本号]
# 示例: bash build-and-push.sh        # 推送 latest
#       bash build-and-push.sh v1.2.0 # 推送 v1.2.0 + latest

set -e

IMAGE="darkver8/tunnel-all"
VERSION="${1:-latest}"

echo "========================================="
echo "  tunnel-all 镜像构建 & 推送"
echo "========================================="
echo ""

# 检查是否在项目根目录
if [[ ! -f "Dockerfile" ]]; then
    echo "[ERROR] 未找到 Dockerfile，请在项目根目录运行此脚本"
    exit 1
fi

# 检查 Docker
if ! command -v docker &>/dev/null; then
    echo "[ERROR] Docker 未安装"
    exit 1
fi

# 先拉取最新代码
echo "[1/4] 拉取最新代码..."
git pull --ff-only 2>/dev/null || true

# 构建镜像
echo "[2/4] 构建镜像 ${IMAGE}:${VERSION} ..."
docker build -t "${IMAGE}:${VERSION}" .

# 如果有版本号，额外打 latest 标签
if [[ "$VERSION" != "latest" ]]; then
    echo "[3/4] 打 latest 标签..."
    docker tag "${IMAGE}:${VERSION}" "${IMAGE}:latest"
else
    echo "[3/4] 跳过标签（已是 latest）"
fi

# 推送
echo "[4/4] 推送到 Docker Hub..."
docker push "${IMAGE}:${VERSION}"
if [[ "$VERSION" != "latest" ]]; then
    docker push "${IMAGE}:latest"
fi

echo ""
echo "========================================="
echo "  构建推送完成！"
echo "  ${IMAGE}:${VERSION}"
echo "========================================="
