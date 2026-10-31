# 1) xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:latest AS xray

# 2) 构建前端
FROM node:20-slim AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 3) 构建 Go 后端
FROM golang:alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go-backend/go.mod go-backend/go.sum ./
RUN go env -w GOTOOLCHAIN=auto && go mod download
COPY go-backend/ ./
RUN go env -w GOTOOLCHAIN=auto && CGO_ENABLED=0 go build -ldflags="-s -w" -o xray-panel ./cmd/server

# 4) 极简运行时
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=xray /usr/local/bin/xray    /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
COPY --from=builder /build/xray-panel   /xray-panel
COPY --from=web /web/dist               /frontend_dist/
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
ENV PANEL_LISTEN=0.0.0.0
ENV PANEL_PORT=2017
ENV PANEL_DATA_DIR=/data/xray
EXPOSE 2017 10808 10809
ENTRYPOINT ["/xray-panel"]
