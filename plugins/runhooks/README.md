# 巡流 NetLoom（runhooks 插件）

## v1.1.27（2026-11-26）
修复「运维工具箱 → 编写」页背景过黑、文字看不清：该子页曾硬编码深黑色底，
与全站浅色主题（深色前景字）冲突。现改为白底/浅灰底 + 深色字，强调色换深色系，
与工具箱其它页一致。纯样式改动，逻辑、API、`data-testid` 不变，升级保留配置。

### 前端改动后的完整发布链
1. 改 `go/web/src/**` → `cd plugins/runhooks/go/web && npm run build`
2. `rm -rf ../internal/webui/dist && mkdir -p ../internal/webui/dist && cp -a dist/. ../internal/webui/dist/`
   （Go `//go:embed all:dist` 读的是 `internal/webui/dist`，不是 `web/dist`）
3. 改 `plugins/runhooks/metadata.json` 的 `version`
4. `cd plugins/runhooks/go && make build-go`（VERSION 单点读 metadata）
5. `cp bin/runhooks ../bins/runhooks-x86_64`（静态门禁：PT_INTERP 必须为空）
6. `python3 build.py <src> <out>`（version-gate 校验引擎内嵌版本 == metadata）

### 本地渲染自测
`dist/index.html` 的资源路径带 `/plugin_proxy/runhooks/` 前缀，静态 server 需剥掉该前缀，
否则 JS 404、React 不挂载。用 `#tool=rest` hash 深链直达「编写」页：
`chromium --headless --no-sandbox --virtual-time-budget=8000 --window-size=1280,900 --screenshot=...`
再用 PIL 主体灰度 + 裁剪放大 tesseract OCR 验证背景与文字可读性。
