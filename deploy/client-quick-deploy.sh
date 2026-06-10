#!/bin/bash
set -euo pipefail

# =============================================================================
# tunnel-all 客户端快速部署脚本
# 用途: 一键安装 Docker（如需要）并部署 tunnel-client 客户端
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
ALIYUN_DOCKER_MIRROR="https://docker.m.daocloud.io"
ALIYUN_DOCKER_MIRROR_2="https://rrsr9a7n.mirror.aliyuncs.com"

# ========== 函数 ==========

check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "请使用 root 用户或 sudo 运行此脚本"
        exit 1
    fi
}

detect_os() {
    if [[ -f /etc/os-release ]]; then
        eval "$(. /etc/os-release && echo "OS=\$ID")" || true
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

docker_installed() {
    command -v docker &>/dev/null
}

docker_compose_installed() {
    docker compose version &>/dev/null || docker-compose --version &>/dev/null
}

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
    setup_docker_mirror

    case "$OS" in
        centos|rocky|alma|rhel)
            install_docker_on_centos
            ;;
        ubuntu|debian|mint|pop)
            install_docker_on_ubuntu_debian
            ;;
        *)
            error "不支持的系统: $OS"
            error "请手动安装 Docker: https://docs.docker.com/engine/install/"
            exit 1
            ;;
    esac

    systemctl enable docker
    systemctl start docker
    info "Docker 安装完成"
    docker --version
    docker compose version
}

# 检查容器是否已运行
container_is_running() {
    local name="$1"
    docker ps --format '{{.Names}}' | grep -q "^${name}$"
}

container_is_exited() {
    local name="$1"
    docker ps -a --format '{{.Names}}:{{.Status}}' | grep -q "^${name}:"
}

deploy_client() {
    step "部署 tunnel-client 客户端"

    local deploy_dir="$(cd "$(dirname "$0")" && pwd)"
    local compose_dir=""

    # 检查 compose 文件位置
    if [[ -f "compose.yml" ]]; then
        compose_dir="."
    elif [[ -f "$deploy_dir/deploy/client/compose.yml" ]]; then
        compose_dir="$deploy_dir/deploy/client"
    else
        error "找不到 deploy/client/compose.yml，请检查项目目录结构"
        exit 1
    fi

    cd "$compose_dir"

    # 交互式配置
    echo ""
    echo "========== 客户端配置 =========="
    echo ""
    info "请提供总控后台的地址和账号信息"
    echo ""

    read -rp "总控地址 [http://192.168.6.64:8088]: " CONTROL_URL
    read -rp "客户端用户名 []: " TUNNEL_USER
    read -rp "客户端密码 []: " TUNNEL_PASSWORD

    CONTROL_URL="${CONTROL_URL:-http://192.168.6.64:8088}"
    if [[ -z "$TUNNEL_USER" ]]; then
        error "用户名不能为空"
        exit 1
    fi
    if [[ -z "$TUNNEL_PASSWORD" ]]; then
        error "密码不能为空"
        exit 1
    fi

    # 验证 URL 格式
    if [[ ! "$CONTROL_URL" =~ ^https?:// ]]; then
        warn "地址格式异常，建议加上 http:// 或 https://"
    fi

    # 生成 .env
    cat > .env <<EOF
CONTROL_URL=$CONTROL_URL
TUNNEL_USER=$TUNNEL_USER
TUNNEL_PASSWORD=$TUNNEL_PASSWORD
EOF
    info "配置文件已生成: .env"

    # 处理已存在的容器
    if container_is_running tunnel-client; then
        warn "发现运行中的 tunnel-client 容器，正在停止并移除..."
        docker compose down
    elif container_is_exited tunnel-client; then
        warn "发现已停止的 tunnel-client 容器，正在移除..."
        docker rm tunnel-client
    fi

    # 拉取镜像
    info "拉取最新镜像..."
    docker pull darkver8/tunnel-client:latest

    # 启动
    info "启动客户端..."
    docker compose up -d

    # 等待验证
    step "等待客户端启动..."
    for i in $(seq 1 30); do
        if docker ps --format '{{.Names}}' | grep -q "^tunnel-client$"; then
            echo ""
            echo "=========================================="
            echo -e "  ${GREEN}✓ 客户端部署成功！${NC}"
            echo "=========================================="
            echo ""
            echo "  总控地址:   $CONTROL_URL"
            echo "  客户端账号: $TUNNEL_USER"
            echo ""
            echo "  常用命令:"
            echo "    查看状态: docker compose ps"
            echo "    查看日志: docker compose logs -f tunnel-client"
            echo "    重启服务: docker compose restart tunnel-client"
            echo "    停止服务: docker compose down"
            echo "=========================================="
            return 0
        fi
        sleep 1
    done

    error "客户端未正常启动，请查看日志:"
    docker compose logs --tail=50
    exit 1
}

# ========== 主流程 ==========

main() {
    step "tunnel-all 客户端快速部署"
    echo ""

    check_root
    detect_os
    install_docker
    deploy_client
}

main "$@"
