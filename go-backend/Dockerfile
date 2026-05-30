FROM ghcr.io/xtls/xray-core:latest AS xray

FROM golang:1.23-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o xray-panel ./cmd/server

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
ENTRYPOINT ["/xray-panel"]
