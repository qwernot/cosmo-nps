FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY upstream ./upstream
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tunnel-control ./cmd/tunnel-control

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/tunnel-control /usr/local/bin/tunnel-control
COPY deploy/docker/frp/frps.toml /opt/tunnel-control/defaults/frp/frps.toml
COPY deploy/docker/nps/conf /opt/tunnel-control/defaults/nps/conf
COPY upstream/NPS/web /opt/tunnel-control/defaults/nps/web
COPY deploy/docker/entrypoint.sh /usr/local/bin/tunnel-control-entrypoint
RUN chmod +x /usr/local/bin/tunnel-control-entrypoint && mkdir -p /app/data

EXPOSE 8088 17000 18024 18025 9080 9443
ENTRYPOINT ["/usr/local/bin/tunnel-control-entrypoint"]
