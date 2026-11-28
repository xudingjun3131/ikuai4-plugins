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

## v1.1.28（2026-11-26）
修复开启代理后 github.com / v2ex.com / google.com 等站点打不开、x.com 能打开。

- 根因：MITM leaf 证书有效期 24h，但 `Authority.Certificate()` 命中内存缓存直接返回、
  从不检查 `NotAfter`；引擎连续运行超过 24h 后证书全部过期，浏览器报证书错误。
  HSTS 站点不允许「继续前往」故打不开，非 HSTS 站点可手动绕过故看似正常。
- 修复：缓存命中校验剩余有效期，已过期或不足 1h 按需重签，写锁双检防并发；
  未过期仍复用缓存。新增 3 个回归测试（FAIL→PASS 坐实）。
- 自测：`go test ./...`（ca 全绿）、`go vet ./...`、`go test -race ./internal/ca`；
  本机新引擎端到端 `/api/health` 1.1.28、leaf notAfter=now+24h、四域名走代理全通。
- 升级保留配置，规则与订阅不丢。

## v1.1.29（2026-11-26）
界面调整：观测台顶部 ribbon 的「全屏观测台 / 退出全屏」按钮从中间移到最右侧，
排在「CA 证书」之后。纯按钮顺序调整，功能 / `data-testid` / 点击行为不变。
Chromium 真渲染复核 8 个按钮坐标：CA x=1288、全屏 x=1386（最后一个）。升级保留配置。

## v1.1.30（2026-11-28）
设置项整体迁入运维工具箱并做页面适配。

- 原插件页「设置与使用说明」弹窗的 7 组选项全部搬到工具箱新增的「设置」分区，改为
  左分区导航 + 右卡片表单布局；原弹窗 DOM 与悬停展开死 CSS 已删除。
- 固件 API 只在插件页可达，故扩展 postMessage 桥接：`settings-request` 回传配置快照、
  `settings-apply` 按分区白名单保存、`settings-action` 代理即时动作，全部复用原有 Vue 方法。
- 引擎未启动时的离线入口改为先拉起引擎再用 `#tool=settings` 深链进入设置页。
- 自测：`npm run build`（TS + vite）通过；Chromium 真渲染复核 7 分区、快照回填、
  次级工具含「设置」、无 JS 报错；父页面 + iframe 双层环境复核桥接消息
  （`settings-apply(ports/autostart)`、`settings-action(refresh-devices)`）全部到达。
- 升级保留配置，规则与订阅不丢。
