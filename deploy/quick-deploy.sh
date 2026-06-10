#!/bin/bash
set -euo pipefail

# =============================================================================
# tunnel-all 总控快速部署脚本
# 用途: 一键安装 Docker（如需要）并部署 tunnel-all 总控服务
# 环境: CentOS 7+/Ubuntu 20.04+/Debian 11+
# =============================================================================

# ---------- 颜色输出 ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${CYAN}[STEP]${NC}  $*"; }

# ---------- 配置区 ----------
# 阿里云 Docker 镜像加速地址
ALIYUN_DOCKER_MIRROR="https://docker.m.daocloud.io"
ALIYUN_DOCKER_MIRROR_2="https://rrsr9a7n.mirror.aliyuncs.com"

# 总控服务配置（可根据需要修改默认值）
DEFAULT_PUBLIC_ADDR=""
DEFAULT_ADMIN_USER="admin"
DEFAULT_ADMIN_PASSWORD="$(openssl rand -base64 12 2>/dev/null || echo 'change-this-password')"

# ========== 函数 ==========

check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "请使用 root 用户或 sudo 运行此脚本"
        exit 1
    fi
}

detect_os() {
    if [[ -f /etc/os-release ]]; then
        eval "$(. /etc/os-release && echo "$ID")" || true
        VERSION_ID=$(grep '^VERSION_ID=' /etc/os-release | cut -d'"' -f2)
    elif [[ -f /etc/redhat-release ]]; then
        OS="centos"
        VERSION_ID="7"
    else
        error "无法识别操作系统"
        exit 1
    fi
    info "检测到系统: $OS $VERSION_ID"
}

docker_compose_installed() {
    docker compose version &>/dev/null || docker-compose --version &>/dev/null
}

docker_installed() {
    command -v docker &>/dev/null
}

# 阿里云镜像加速配置
setup_docker_mirror() {
    step "配置 Docker 阿里云镜像加速"
    local mirror_dir="/etc/docker"
    mkdir -p "$mirror_dir"

    cat > "$mirror_dir/daemon.json" <<EOF
{
  "registry-mirrors": [
    "$ALIYUN_DOCKER_MIRROR",
    "$ALIYUN_DOCKER_MIRROR_2"
  ],
  "exec-args": ["--default-ulimit=nofile=8192:65536"]
}
EOF
    info "已配置镜像加速: $ALIYUN_DOCKER_MIRROR"
    warn "请根据阿里云控制台获取您的专属加速地址替换上述地址"

    if systemctl is-active docker &>/dev/null; then
        systemctl restart docker
        info "已重启 Docker 服务"
    fi
}

# ---- Docker 安装 ----

install_docker_on_centos() {
    step "安装 Docker (CentOS)"
    yum install -y yum-utils
    yum-config-manager --add-repo https://mirrors.aliyun.com/docker-ce/linux/centos/docker-ce.repo
    yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_docker_on_ubuntu_debian() {
    step "安装 Docker (Ubuntu/Debian)"
    apt-get update
    apt-get install -y ca-certificates curl gnupg

    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/$OS/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc

    echo \
      "deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
      https://mirrors.aliyun.com/docker-ce/linux/$OS \
      \$(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
      tee /etc/apt/sources.list.d/docker.list > /dev/null

    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_docker() {
    if docker_installed && docker_compose_installed; then
        info "Docker 已安装"
        docker --version
        info "Docker Compose 已安装"
        docker compose version
        # 检查镜像配置
        if [[ -f /etc/docker/daemon.json ]]; then
            if grep -q "aliyuncs.com\|daocloud" /etc/docker/daemon.json; then
                info "镜像加速已配置"
            else
                warn "未检测到阿里云镜像加速，正在配置..."
                setup_docker_mirror
            fi
        else
            info "镜像加速配置不存在，正在配置..."
            setup_docker_mirror
        fi
        return 0
    fi

    info "未检测到 Docker，开始安装..."

    # 设置镜像加速（安装前先配）
    setup_docker_mirror

    case "$OS" in
        centos|rocky|alma|rhel)
            install_docker_on_centos
            ;;
        ubuntu|debian|mint|pop)
            install_docker_on_ubuntu_debian
            ;;
        *)
            # 通用安装（从官方脚本报错提示）
            error "不支持的系统: $OS"
            error "请手动安装 Docker: https://docs.docker.com/engine/install/"
            exit 1
            ;;
    esac

    # 启动并设置开机自启
    systemctl enable docker
    systemctl start docker
    info "Docker 安装完成"
    docker --version
    docker compose version
}

# ---- 总控部署 ----

deploy_control() {
    step "部署 tunnel-control 总控服务"

    local deploy_dir="$(cd "$(dirname "$0")" && pwd)"
    local compose_dir=""

    # 检查是否在 deploy/docker 目录
    if [[ -f "compose.yml" && -f ".env.example" ]]; then
        deploy_dir="$(pwd)"
        compose_dir="."
    elif [[ -f "$deploy_dir/deploy/docker/compose.yml" ]]; then
        compose_dir="$deploy_dir/deploy/docker"
    else
        error "找不到 deploy/docker/compose.yml，请检查项目目录结构"
        exit 1
    fi

    # 交互式配置
    echo ""
    echo "========== 总控配置 =========="
    read -rp "公网IP/域名 [留空=本机IP]: " PUBLIC_ADDR
    read -rp "管理员用户名 [$DEFAULT_ADMIN_USER]: " ADMIN_USER
    read -rp "管理员密码 [$DEFAULT_ADMIN_PASSWORD]: " ADMIN_PASSWORD

    PUBLIC_ADDR="${PUBLIC_ADDR:-$(curl -s ifconfig.me 2>/dev/null || curl -s icanhazip.com 2>/dev/null || hostname -I | awk '{print $1}')}${PUBLIC_ADDR}"
    [[ -z "$PUBLIC_ADDR" ]] && PUBLIC_ADDR="$(hostname -I | awk '{print $1}')"
    ADMIN_USER="${ADMIN_USER:-$DEFAULT_ADMIN_USER}"
    ADMIN_PASSWORD="${ADMIN_PASSWORD:-$DEFAULT_ADMIN_PASSWORD}"

    cd "$compose_dir"

    # 备份旧配置
    if [[ -d "data" ]]; then
        warn "检测到已有 data 目录，将备份到 backups/"
        local ts
        ts=$(date +%Y%m%d-%H%M%S)
        mkdir -p backups
        cp -r data backups/data-$ts
        info "已备份: backups/data-$ts"
    fi

    # 生成 .env
    cat > .env <<EOF
PUBLIC_ADDR=$PUBLIC_ADDR
ADMIN_USER=$ADMIN_USER
ADMIN_PASSWORD=$ADMIN_PASSWORD
EOF
    info "配置文件已生成: .env"

    # 拉取镜像
    info "拉取最新镜像..."
    docker pull darkver8/tunnel-all:latest

    # 启动
    info "启动服务..."
    docker compose up -d

    # 等待健康检查
    step "等待服务启动..."
    for i in $(seq 1 30); do
        if curl -fsS http://127.0.0.1:8088/healthz &>/dev/null; then
            echo ""
            echo "=========================================="
            echo -e "  ${GREEN}✓ 总控部署成功！${NC}"
            echo "=========================================="
            echo ""
            echo "  访问地址: http://$PUBLIC_ADDR:8088"
            echo "  用户名:   $ADMIN_USER"
            echo "  密码:     $ADMIN_PASSWORD"
            echo ""
            echo "  常用命令:"
            echo "    查看状态: docker compose ps"
            echo "    查看日志: docker compose logs -f tunnel-stack"
            echo "    重启服务: docker compose restart tunnel-stack"
            echo "    停止服务: docker compose down"
            echo "=========================================="
            return 0
        fi
        sleep 1
    done

    error "服务未正常启动，请查看日志:"
    docker compose logs --tail=50
    exit 1
}

# ========== 主流程 ==========

main() {
    step "tunnel-all 总控快速部署"
    echo ""

    check_root
    detect_os
    install_docker
    deploy_control
}

main "$@"
