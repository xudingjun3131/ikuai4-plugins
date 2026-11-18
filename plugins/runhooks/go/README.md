# runhooks 引擎源码(iKuai 插件内置 Go 引擎)

本目录是 `plugins/runhooks` 插件的引擎源码(模块 `github.com/GitterBois/runhooks`,
fork 自 reqable 引擎并增加规则订阅子系统)。**发货的二进制是 `bins/runhooks-x86_64`,
本目录仅作可维护性收录, 不参与 ipk 打包**(build.py 只打 html/script/bins/install.sh)。

## 构建(在 x86_64 Linux 构建机上)

```bash
cd go
# WebUI(需要 node; 也可跳过用已提交的 internal/webui/dist 走 build-go)
ln -sfn <node_modules目录> web/node_modules     # 依赖与 reqable 完全一致
make build-web          # web/ → internal/webui/dist
# 引擎(静态是硬门槛: 设备固件只有 uClibc loader, 没有 glibc loader)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bin/runhooks ./cmd/proxying
cp bin/runhooks ../bins/runhooks-x86_64
```

**每次重建后必须做 PT_INTERP 门禁**(漏了就是设备上 "No such file or directory"
启动失败, 且重装/升级/改路径全部无效):

```bash
python3 - <<'EOF'
import struct
d=open('bin/runhooks','rb').read()
ph=struct.unpack('<Q',d[32:40])[0]; es=struct.unpack('<H',d[54:56])[0]; n=struct.unpack('<H',d[56:58])[0]
for i in range(n):
    o=ph+i*es
    if struct.unpack('<I',d[o:o+4])[0]==3:
        po=struct.unpack('<Q',d[o+8:o+16])[0]; pz=struct.unpack('<Q',d[o+32:o+40])[0]
        print('PT_INTERP =', d[po:po+pz].rstrip(b'\x00').decode()); raise SystemExit(1)
print('STATIC-OK')
EOF
```

## 测试

```bash
go test ./internal/...                                  # 单元测试(解析/适配/转换/分层)
bash scripts/smoke_subscription.sh                      # 订阅链路端到端(回环)
bash scripts/check_ui.sh                                # WebUI 内嵌核查
bash scripts/netns_plugin_test.sh                       # 插件 service.sh netns 隔离测试
```

## 结构(相对 reqable 的增量)

- `internal/subscription/` — **新增**: QuanX/Surge/纯列表解析(`parse.go`)、
  适配矩阵(`adapt.go`)、QX 脚本转换(`qxscript.go`)、订阅管理器(`manager.go`)
- `internal/rules/rule.go` — 新增 `rules.sub.json` 订阅层(Save 只落用户规则,
  SaveSubRules 只写订阅层); hostMatch 支持 `*kw*` 中间通配; body_replace
  From 为空=整包替换; map_local 支持内联 Body
- `internal/proxy/scope.go` — 新增 `scope.sub.json` 订阅层合并(订阅域名=白名单语义)
- `internal/api/subscriptions.go` — `/api/subscriptions` 系列端点
- `web/src/components/subs-panel.tsx` — WebUI 订阅管理面板
