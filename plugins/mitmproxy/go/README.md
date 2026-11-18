# mitmproxy-go — mitmproxy 插件的 Go 引擎（v2.0.0 起重写）

自研 Go MITM 引擎，替代原 Alpine Python3.14 + mitmproxy 11 运行时（~100MB → ~19MB 静态单文件）。
架构移植自 reqable 引擎（同一团队代码，显式/透明双模式、flow 存储、规则、断点、REST、脚本、SSE、内嵌 React WebUI）。

## 构建

```bash
cd web && npm ci && npm run build && cd ..        # 前端 → web/dist
mkdir -p internal/webui/dist && cp -a web/dist/. internal/webui/dist/
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=2.0.0" -o ../bins/mitmproxy-go ./cmd/proxying
```

## 与插件的关系

- 插件 `bins/mitmproxy-go` 即本仓库产物（`plugin-mitmproxy-x86_64-v2.0.0-*.ipk` 内含同 md5 二进制）
- `script/service.sh` 以固定参数启动：`-proxy-addr 0.0.0.0:18070 -transparent-addr 0.0.0.0:18072 -api-addr <bind>:18071 -data-dir <配置目录> -transparent`
- WebUI 经路由器同源 `/plugin_proxy/mitmproxy/` 代理（固件 OpenResty Lua 校验 sess_key），引擎同时挂载裸 `/api/*`（service.sh 本地调用）与前缀路径
- API 语义兼容 mitmweb：`/api/flows`、`/api/capture`(PUT)、`/api/ca.pem`、`/api/events`(SSE)

## 关键差异（相对原 Python 版）

| | Python 版(≤1.0.7) | Go 版(2.0.0) |
|---|---|---|
| 运行时 | Alpine+Python3.14+mitmproxy(~100MB) | 静态 Go 二进制(~19MB) |
| 冷启动 | 最长 60s 健康等待 | 毫秒级(API 首响 ~60ms) |
| WebUI 语言 | mitmweb 英文+注入式汉化 | 中文原生 |
| 选项 | 89 个上游选项词典汉化 | 引擎自有选项 UI |
