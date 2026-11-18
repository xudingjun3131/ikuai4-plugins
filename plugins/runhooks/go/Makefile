.PHONY: build build-web build-go test run clean release

BINARY := bin/runhooks
CMD    := ./cmd/proxying
WEB    := web
UI_DIST := internal/webui/dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 1.0.0)
LDFLAGS := -X main.Version=$(VERSION)

build-web:
	cd $(WEB) && npm ci && npm run build
	rm -rf $(UI_DIST)
	mkdir -p $(UI_DIST)
	cp -a $(WEB)/dist/. $(UI_DIST)/

# [static-gate] 目标固件(iKuai uClibc)只有 uClibc loader, 没有 glibc loader:
# 动态二进制 exec 时内核找不到 PT_INTERP → ENOENT → bash 报
# "No such file or directory"(指的是 loader 不是二进制本身, 极常误判)。
# 2026-11-18 实锤: v1.0.42 重建时漏了 CGO_ENABLED=0, 发货动态包, 设备全数启动失败。
# 这三个 flags 一个都不能删; 每次构建后必须复查 PT_INTERP 为空。
build: build-web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS) -s -w" -o $(BINARY) $(CMD)

# Go-only build using committed minimal UI embed (no npm).
build-go:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS) -s -w" -o $(BINARY) $(CMD)

test:
	go test ./...

run: build
	./$(BINARY)

release: build-web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS) -s -w" -o $(BINARY) $(CMD)
	@echo "built $(BINARY) version $(VERSION)"

clean:
	rm -f $(BINARY)
