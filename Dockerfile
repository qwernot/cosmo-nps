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

FROM alpine:3.20 AS control

WORKDIR /app
COPY --from=builder /out/tunnel-control /usr/local/bin/tunnel-control
COPY deploy/docker/entrypoint.sh /usr/local/bin/tunnel-control-entrypoint
RUN chmod +x /usr/local/bin/tunnel-control-entrypoint && mkdir -p /app/data

EXPOSE 8088
ENTRYPOINT ["/usr/local/bin/tunnel-control-entrypoint"]

FROM alpine:3.20 AS node

WORKDIR /app
COPY --from=builder /out/tunnel-agent /usr/local/bin/tunnel-agent
COPY deploy/docker/nps/conf /opt/tunnel-control/defaults/nps/conf
COPY upstream/NPS/web /opt/tunnel-control/defaults/nps/web
RUN mkdir -p /app/data

EXPOSE 18024 18025 18089 9080 9443
ENTRYPOINT ["/usr/local/bin/tunnel-agent"]

FROM alpine:3.20 AS client

WORKDIR /app
COPY --from=builder /out/tunnel-client /usr/local/bin/tunnel-client
RUN mkdir -p /app/data

ENTRYPOINT ["/usr/local/bin/tunnel-client"]
