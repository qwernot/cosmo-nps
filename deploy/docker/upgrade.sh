#!/bin/sh
set -eu

IMAGE="${IMAGE:-darkver8/cosmo-nps:latest}"
SERVICE="${SERVICE:-tunnel-stack}"
COMPOSE="${COMPOSE:-docker compose}"
DATA_DIR="${DATA_DIR:-./data}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"

cd "$(dirname "$0")"

if [ -f .env ]; then
  # shellcheck disable=SC1091
  . ./.env
fi

CONTROL_PORT="${CONTROL_PORT:-8088}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${CONTROL_PORT}/healthz}"

timestamp="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

if [ -d "$DATA_DIR" ]; then
  backup_file="$BACKUP_DIR/data-$timestamp.tar.gz"
  tar -czf "$backup_file" -C "$DATA_DIR" .
  echo "backup: $backup_file"
else
  echo "backup: skipped, $DATA_DIR does not exist"
fi

echo "pull: $IMAGE"
docker pull "$IMAGE"

echo "remove existing container if conflict"
docker rm -f "$SERVICE" || true

echo "restart: $SERVICE"
$COMPOSE up -d "$SERVICE"

echo "health: $HEALTH_URL"
for i in $(seq 1 30); do
  if curl -fsS "$HEALTH_URL" >/dev/null; then
    echo "health: ok"
    $COMPOSE ps "$SERVICE"
    exit 0
  fi
  sleep 1
done

echo "health: failed"
$COMPOSE logs --tail=80 "$SERVICE" || true
exit 1
