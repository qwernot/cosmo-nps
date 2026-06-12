#!/bin/bash
set -euo pipefail

# Cosmo NPS total-control quick deploy script.
# Supports CentOS/RHEL/Rocky/Alma, Ubuntu/Debian/Mint/Pop.

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${CYAN}[STEP]${NC}  $*"; }

IMAGE="${IMAGE:-darkver8/cosmo-nps:latest}"
SERVICE="${SERVICE:-tunnel-stack}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8088/healthz}"
DEFAULT_ADMIN_USER="${DEFAULT_ADMIN_USER:-admin}"
DEFAULT_ADMIN_PASSWORD="${DEFAULT_ADMIN_PASSWORD:-$(openssl rand -base64 12 2>/dev/null || echo 'change-this-password')}"
SETUP_DOCKER_MIRROR="${SETUP_DOCKER_MIRROR:-0}"
ALIYUN_DOCKER_MIRROR="${ALIYUN_DOCKER_MIRROR:-https://2bc9dt6w.mirror.aliyuncs.com}"
ALIYUN_DOCKER_MIRROR_2="${ALIYUN_DOCKER_MIRROR_2:-https://docker.1ms.run}"

OS=""
VERSION_ID=""
COMPOSE_CMD=""

check_root() {
    if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
        error "Please run this script as root, for example: sudo bash deploy/quick-deploy.sh"
        exit 1
    fi
}

detect_os() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        OS="${ID:-}"
        case "$OS" in
            centos|rocky|alma|rhel|ubuntu|debian|mint|pop) ;;
            *)
                for like in ${ID_LIKE:-}; do
                    case "$like" in
                        centos|rhel|rocky|alma) OS="centos"; break ;;
                        ubuntu|debian) OS="ubuntu"; break ;;
                    esac
                done
                ;;
        esac
        VERSION_ID="${VERSION_ID:-unknown}"
    elif [[ -f /etc/redhat-release ]]; then
        OS="centos"
        VERSION_ID="7"
    else
        error "Cannot detect operating system."
        exit 1
    fi
    info "Detected OS: $OS $VERSION_ID"
}

docker_installed() {
    command -v docker >/dev/null 2>&1
}

detect_compose() {
    if docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        COMPOSE_CMD="docker-compose"
    else
        COMPOSE_CMD=""
        return 1
    fi
}

setup_docker_mirror() {
    [[ "$SETUP_DOCKER_MIRROR" == "1" ]] || return 0

    step "Configuring Docker registry mirrors"
    local mirror_dir="/etc/docker"
    local daemon_json="$mirror_dir/daemon.json"
    mkdir -p "$mirror_dir"

    if [[ -f "$daemon_json" ]] && command -v python3 >/dev/null 2>&1; then
        python3 - "$daemon_json" "$ALIYUN_DOCKER_MIRROR" "$ALIYUN_DOCKER_MIRROR_2" <<'PY'
import json
import sys

path, mirror1, mirror2 = sys.argv[1:]
try:
    with open(path, "r", encoding="utf-8") as f:
        config = json.load(f)
except Exception:
    config = {}
config["registry-mirrors"] = [mirror1, mirror2]
with open(path, "w", encoding="utf-8") as f:
    json.dump(config, f, indent=2)
PY
    else
        cat > "$daemon_json" <<EOF
{
  "registry-mirrors": [
    "$ALIYUN_DOCKER_MIRROR",
    "$ALIYUN_DOCKER_MIRROR_2"
  ]
}
EOF
    fi

    if systemctl is-active docker >/dev/null 2>&1; then
        systemctl restart docker
    fi
}

install_docker_on_centos() {
    step "Installing Docker on CentOS/RHEL"
    yum install -y yum-utils
    yum-config-manager --add-repo https://mirrors.aliyun.com/docker-ce/linux/centos/docker-ce.repo
    yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_docker_on_ubuntu_debian() {
    step "Installing Docker on Ubuntu/Debian"
    apt-get update
    apt-get install -y ca-certificates curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://mirrors.aliyun.com/docker-ce/linux/$OS/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://mirrors.aliyun.com/docker-ce/linux/$OS $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
        > /etc/apt/sources.list.d/docker.list
    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_docker() {
    if docker_installed && detect_compose; then
        info "Docker is installed: $(docker --version)"
        info "Compose is installed: $($COMPOSE_CMD version | head -n 1)"
        setup_docker_mirror
        return 0
    fi

    info "Docker or Compose was not found. Installing..."
    setup_docker_mirror
    case "$OS" in
        centos|rocky|alma|rhel) install_docker_on_centos ;;
        ubuntu|debian|mint|pop) install_docker_on_ubuntu_debian ;;
        *)
            error "Unsupported OS: $OS"
            error "Please install Docker manually: https://docs.docker.com/engine/install/"
            exit 1
            ;;
    esac
    systemctl enable docker
    systemctl start docker
    detect_compose || {
        error "Docker Compose was not installed correctly."
        exit 1
    }
    info "Docker installed: $(docker --version)"
    info "Compose installed: $($COMPOSE_CMD version | head -n 1)"
}

resolve_public_addr() {
    local value="${PUBLIC_ADDR:-}"
    if [[ -n "$value" ]]; then
        echo "$value"
        return 0
    fi
    value="$(curl -fsS ifconfig.me 2>/dev/null || curl -fsS icanhazip.com 2>/dev/null || hostname -I | awk '{print $1}')"
    if [[ -z "$value" ]]; then
        error "Cannot resolve PUBLIC_ADDR automatically. Please set PUBLIC_ADDR manually."
        exit 1
    fi
    echo "$value"
}

prompt_if_needed() {
    if [[ "${NONINTERACTIVE:-0}" == "1" ]]; then
        PUBLIC_ADDR="$(resolve_public_addr)"
        ADMIN_USER="${ADMIN_USER:-$DEFAULT_ADMIN_USER}"
        ADMIN_PASSWORD="${ADMIN_PASSWORD:-$DEFAULT_ADMIN_PASSWORD}"
        return 0
    fi

    echo ""
    echo "========== Cosmo NPS total-control config =========="
    read -rp "Public IP/domain [blank=auto]: " PUBLIC_ADDR_INPUT
    read -rp "Admin username [$DEFAULT_ADMIN_USER]: " ADMIN_USER_INPUT
    read -rp "Admin password [$DEFAULT_ADMIN_PASSWORD]: " ADMIN_PASSWORD_INPUT

    PUBLIC_ADDR="${PUBLIC_ADDR_INPUT:-${PUBLIC_ADDR:-}}"
    PUBLIC_ADDR="$(resolve_public_addr)"
    ADMIN_USER="${ADMIN_USER_INPUT:-${ADMIN_USER:-$DEFAULT_ADMIN_USER}}"
    ADMIN_PASSWORD="${ADMIN_PASSWORD_INPUT:-${ADMIN_PASSWORD:-$DEFAULT_ADMIN_PASSWORD}}"
}

write_env_value() {
    local key="$1"
    local value="$2"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '%s="%s"\n' "$key" "$value"
}

write_env_file() {
    umask 077
    {
        write_env_value PUBLIC_ADDR "$PUBLIC_ADDR"
        write_env_value ADMIN_USER "$ADMIN_USER"
        write_env_value ADMIN_PASSWORD "$ADMIN_PASSWORD"
    } > .env
    chmod 600 .env
}

backup_data_dir() {
    [[ -d data ]] || return 0
    local ts backup_file
    ts="$(date +%Y%m%d-%H%M%S)"
    mkdir -p backups
    backup_file="backups/data-$ts.tar.gz"
    tar -czf "$backup_file" -C data .
    info "Existing data backed up to: $backup_file"
}

backup_env_file() {
    [[ -f .env ]] || return 0
    local ts backup_file
    ts="$(date +%Y%m%d-%H%M%S)"
    mkdir -p backups
    backup_file="backups/env-$ts"
    cp .env "$backup_file"
    info "Existing .env backed up to: $backup_file"
}

find_compose_dir() {
    local script_dir
    script_dir="$(cd "$(dirname "$0")" && pwd)"
    if [[ -f compose.yml ]]; then
        pwd
    elif [[ -f "$script_dir/docker/compose.yml" ]]; then
        echo "$script_dir/docker"
    else
        error "Cannot find deploy/docker/compose.yml. Please run from the project root or deploy/docker."
        exit 1
    fi
}

deploy_control() {
    step "Deploying Cosmo NPS total-control"
    local compose_dir
    compose_dir="$(find_compose_dir)"
    prompt_if_needed

    cd "$compose_dir"
    mkdir -p data/control data/export backups
    if [[ -d data ]]; then
        warn "Existing data detected. ADMIN_USER/ADMIN_PASSWORD only bootstrap the first admin and will not reset existing users."
    fi
    backup_data_dir
    backup_env_file
    write_env_file
    info "Config written to: $compose_dir/.env"

    info "Pulling image: $IMAGE"
    docker pull "$IMAGE"

    info "Starting service: $SERVICE"
    $COMPOSE_CMD up -d "$SERVICE"

    step "Waiting for service health"
    for _ in $(seq 1 30); do
        if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
            echo ""
            echo "=========================================="
            echo -e "  ${GREEN}Total-control deployed successfully${NC}"
            echo "=========================================="
            echo "  URL:      http://$PUBLIC_ADDR:8088"
            echo "  Username: $ADMIN_USER"
            echo "  Password: $ADMIN_PASSWORD"
            echo ""
            echo "  Commands:"
            echo "    $COMPOSE_CMD ps"
            echo "    $COMPOSE_CMD logs -f $SERVICE"
            echo "    $COMPOSE_CMD restart $SERVICE"
            echo "    $COMPOSE_CMD down"
            echo "=========================================="
            return 0
        fi
        sleep 1
    done

    error "Service did not become healthy. Recent logs:"
    $COMPOSE_CMD logs --tail=80 "$SERVICE" || true
    exit 1
}

main() {
    step "Cosmo NPS total-control quick deploy"
    check_root
    detect_os
    install_docker
    deploy_control
}

main "$@"
