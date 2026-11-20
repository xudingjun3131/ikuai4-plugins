.PHONY: build-go test version clean

BINARY := bin/netfenced
CMD    := ./cmd/netfenced
# 版本唯一真值来源 = 插件 metadata.json(与 build.py version-gate 互为双保险)
VERSION := $(shell python3 -c "import json;print(json.load(open('../metadata.json'))['version'])")
LDFLAGS := -X main.Version=$(VERSION)

# [static-gate] 目标固件 iKuai uClibc 无 glibc loader; PT_INTERP 指向 ld-linux-*
# 的动态二进制在设备上 exec 必报 No such file or directory。三个 flag 缺一不可。
build-go:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS) -s -w" -o $(BINARY) $(CMD)

test:
	go test ./...

version:
	@echo $(VERSION)

clean:
	rm -f $(BINARY)
