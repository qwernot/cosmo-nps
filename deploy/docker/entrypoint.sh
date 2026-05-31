#!/bin/sh
set -eu

DATA_DIR="${DATA_DIR:-/app/data}"
CONTROL_DIR="$DATA_DIR/control"
FRP_DIR="$DATA_DIR/frp"
NPS_DIR="$DATA_DIR/nps"
EXPORT_DIR="$DATA_DIR/export"
NPS_CONF_DIR="$NPS_DIR/conf"

PUBLIC_ADDR="${PUBLIC_ADDR:-127.0.0.1}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
FRP_BIND_PORT="${FRP_BIND_PORT:-17000}"
FRP_DASHBOARD_PORT="${FRP_DASHBOARD_PORT:-0}"
NPS_WEB_PORT="${NPS_WEB_PORT:-0}"
NPS_BRIDGE_PORT="${NPS_BRIDGE_PORT:-18024}"
NPS_TLS_BRIDGE_PORT="${NPS_TLS_BRIDGE_PORT:-18025}"
NPS_HTTP_PORT="${NPS_HTTP_PORT:-9080}"
NPS_HTTPS_PORT="${NPS_HTTPS_PORT:-9443}"
USER_PORT_RANGE="${USER_PORT_RANGE:-10000-20000}"

mkdir -p "$CONTROL_DIR" "$FRP_DIR" "$EXPORT_DIR" "$NPS_CONF_DIR" "$NPS_DIR/data"
rm -rf /conf /data /usr/local/bin/conf
ln -s "$NPS_CONF_DIR" /conf
ln -s "$NPS_DIR/data" /data
ln -s "$NPS_CONF_DIR" /usr/local/bin/conf

if [ ! -f "$FRP_DIR/frps.toml" ]; then
  cp /opt/tunnel-control/defaults/frp/frps.toml "$FRP_DIR/frps.toml"
fi

if [ ! -f "$NPS_CONF_DIR/nps.conf" ]; then
  cp -R /opt/tunnel-control/defaults/nps/conf/. "$NPS_CONF_DIR/"
fi
if [ ! -d "$NPS_DIR/web" ]; then
  cp -R /opt/tunnel-control/defaults/nps/web "$NPS_DIR/web"
fi

if [ ! -s "$FRP_DIR/frps-users.json" ]; then
  printf '[]\n' > "$FRP_DIR/frps-users.json"
fi

sed -i \
  -e "s/^bindPort = .*/bindPort = $FRP_BIND_PORT/" \
  -e "s/^port = .*/port = $FRP_DASHBOARD_PORT/" \
  -e "s/^user = .*/user = \"$ADMIN_USER\"/" \
  -e "s/^password = .*/password = \"$ADMIN_PASSWORD\"/" \
  -e "s#^path = .*#path = \"$FRP_DIR/frps-users.json\"#" \
  -e "s/^adminUser = .*/adminUser = \"$ADMIN_USER\"/" \
  -e "s/^adminPassword = .*/adminPassword = \"$ADMIN_PASSWORD\"/" \
  "$FRP_DIR/frps.toml"

sed -i \
  -e "s/^http_proxy_port=.*/http_proxy_port=$NPS_HTTP_PORT/" \
  -e "s/^https_proxy_port=.*/https_proxy_port=$NPS_HTTPS_PORT/" \
  -e "s/^bridge_port=.*/bridge_port=$NPS_BRIDGE_PORT/" \
  -e "s/^tls_bridge_port=.*/tls_bridge_port=$NPS_TLS_BRIDGE_PORT/" \
  -e "s#^log_path=.*#log_path=$NPS_DIR/data/nps.log#" \
  -e "s/^web_username=.*/web_username=$ADMIN_USER/" \
  -e "s/^web_password=.*/web_password=$ADMIN_PASSWORD/" \
  -e "s/^web_port=.*/web_port=$NPS_WEB_PORT/" \
  -e "s/^web_ip=.*/web_ip=127.0.0.1/" \
  -e "s/^auth_key=.*/auth_key=$ADMIN_PASSWORD/" \
  -e "s/^allow_ports=.*/allow_ports=$USER_PORT_RANGE/" \
  "$NPS_CONF_DIR/nps.conf"

/usr/local/bin/tunnel-control \
  -addr :8088 \
  -db "$CONTROL_DIR/tunnel-control.json" \
  -embedded-engines \
  -public-addr "$PUBLIC_ADDR" \
  -frp-port "$FRP_BIND_PORT" \
  -frp-dashboard-port "$FRP_DASHBOARD_PORT" \
  -nps-port "$NPS_BRIDGE_PORT" \
  -frp-users-path "$FRP_DIR/frps-users.json" \
  -nps-clients-path "$NPS_CONF_DIR/clients.json" \
  -config-out-dir "$EXPORT_DIR" \
  -nps-workdir "$NPS_DIR" \
  -admin-user "$ADMIN_USER" \
  -admin-password "$ADMIN_PASSWORD"
