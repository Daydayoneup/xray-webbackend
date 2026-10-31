# 本地构建单文件二进制（前端嵌入其中）：make build → ./xray-panel
# 交叉编译：make build GOOS=linux GOARCH=arm64
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)
OUT     ?= xray-panel
WEBDIST := go-backend/internal/webui/dist

.PHONY: build web test clean

build: web
	cd go-backend && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o ../$(OUT) ./cmd/server
	@echo "已生成 $(OUT)（$(GOOS)/$(GOARCH)，$(VERSION)）"

web:
	cd frontend && npm ci && npm run build
	rm -rf $(WEBDIST) && mkdir -p $(WEBDIST)
	cp -R frontend/dist/. $(WEBDIST)/
	touch $(WEBDIST)/.gitkeep

test:
	cd go-backend && go test ./...
	cd frontend && npm test

clean:
	rm -f $(OUT)
	find $(WEBDIST) -mindepth 1 ! -name .gitkeep -delete
