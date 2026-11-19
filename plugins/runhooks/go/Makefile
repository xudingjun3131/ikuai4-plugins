.PHONY: build build-web build-go test run clean release version

BINARY := bin/runhooks
CMD    := ./cmd/proxying
WEB    := web
UI_DIST := internal/webui/dist
# [version-single-source] 引擎版本号唯一真值来源 = 插件 metadata.json。
# 原来用 git describe, 结果 /api/health 报的是 commit hash 或上一次手工传入的
# 1.1.2, 而 metadata 与线上清单已到 1.1.4 —— 版本号漂移(测试报告 D-01)。
# 用 := 禁止命令行覆盖: 重编前必须先改 metadata.json 并升 version。
VERSION := $(shell python3 -c "import json;print(json.load(open('../metadata.json'))['version'])")
LDFLAGS := -X main.Version=$(VERSION)

# 构建前打印本次注入的版本, 便于人工核对(与 build.py 的版本门禁互为双保险)。
version:
	@echo $(VERSION)

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
