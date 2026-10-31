# 多架构镜像（linux/amd64、linux/arm64）：前端与 Go 在构建机原生编译（交叉编译 Go），不需要 QEMU。

# 1) xray 二进制 + geodata（按目标架构拉取）
FROM ghcr.io/xtls/xray-core:latest AS xray

# 2) 构建前端
FROM --platform=$BUILDPLATFORM node:20-slim AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 3) 构建 Go 后端（前端嵌入二进制）
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go-backend/go.mod go-backend/go.sum ./
RUN go env -w GOTOOLCHAIN=auto && go mod download
COPY go-backend/ ./
COPY --from=web /web/dist ./internal/webui/dist/
RUN go env -w GOTOOLCHAIN=auto && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o xray-panel ./cmd/server

# 4) 极简运行时
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=xray /usr/local/bin/xray    /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
COPY --from=builder /build/xray-panel   /xray-panel
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
ENV PANEL_LISTEN=0.0.0.0
ENV PANEL_PORT=2017
ENV PANEL_DATA_DIR=/data/xray
EXPOSE 2017 10808 10809
VOLUME ["/data/xray"]
ENTRYPOINT ["/xray-panel"]
