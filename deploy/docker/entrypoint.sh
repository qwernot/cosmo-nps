#!/bin/sh
set -eu

DATA_DIR="${DATA_DIR:-/app/data}"
CONTROL_DIR="$DATA_DIR/control"
EXPORT_DIR="$DATA_DIR/export"

PUBLIC_ADDR="${PUBLIC_ADDR:-127.0.0.1}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"

# Runtime values used when generating configs for the legacy local node.
# Real tunnel endpoints should be defined on each tunnel-agent node.
NPS_BRIDGE_PORT="${NPS_BRIDGE_PORT:-18024}"
NPS_HTTP_PORT="${NPS_HTTP_PORT:-9080}"
NPS_HTTPS_PORT="${NPS_HTTPS_PORT:-9443}"

mkdir -p "$CONTROL_DIR" "$EXPORT_DIR"

set -- \
  -addr :8088 \
  -db "$CONTROL_DIR/tunnel-control.json" \
  -public-addr "$PUBLIC_ADDR" \
  -nps-port "$NPS_BRIDGE_PORT" \
  -nps-http-port "$NPS_HTTP_PORT" \
  -nps-https-port "$NPS_HTTPS_PORT" \
  -nps-clients-path "$CONTROL_DIR/nps-clients.json" \
  -config-out-dir "$EXPORT_DIR" \
  -admin-user "$ADMIN_USER" \
  -admin-password "$ADMIN_PASSWORD"

exec /usr/local/bin/tunnel-control "$@"
