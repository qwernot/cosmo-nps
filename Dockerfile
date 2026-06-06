FROM golang:1.25-alpine AS builder

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod ./
COPY go.sum ./
COPY upstream ./upstream
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tunnel-control ./cmd/tunnel-control \
  && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tunnel-agent ./cmd/tunnel-agent \
  && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tunnel-client ./cmd/tunnel-client

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/tunnel-control /usr/local/bin/tunnel-control
COPY --from=builder /out/tunnel-agent /usr/local/bin/tunnel-agent
COPY --from=builder /out/tunnel-client /usr/local/bin/tunnel-client
COPY deploy/docker/nps/conf /opt/tunnel-control/defaults/nps/conf
COPY upstream/NPS/web /opt/tunnel-control/defaults/nps/web
COPY deploy/docker/entrypoint.sh /usr/local/bin/tunnel-control-entrypoint
RUN chmod +x /usr/local/bin/tunnel-control-entrypoint && mkdir -p /app/data

EXPOSE 8088 18024 18025 18089 9080 9443
ENTRYPOINT ["/usr/local/bin/tunnel-control-entrypoint"]
