# NetFence 一体化防火墙插件 · GitHub 调研与整合设计

调研日期：2026-11-20　执行：运维开发　工作区：`/opt/ikuai4-plugins`

## 一、需求与结论摘要

需求：新建一个「类似山石防火墙 + 上网行为管理一体化」的插件，先调研 GitHub 上相关产品，取各家优点整合出最优版。

**结论：GitHub 上不存在一个可直接搬进 iKuai 插件框架的现成「一体化」成品**，但每个能力层都有成熟、活跃、可借鉴的开源项目。原因是产品形态不匹配：

- pfSense / OPNsense / IPFire 是**整机发行版**（FreeBSD/Linux + 内核栈 + Web 框架），不是可嵌入的组件；
- Suricata / nDPI / OpenAppFilter 依赖 **NFQUEUE / 内核模块 / libpcap**，iKuai 插件是用户态静态二进制，无内核模块加载能力；
- e2guardian / Squid 是**正向代理**，要改客户端配置或做透明重定向。

因此「最优版」不是移植某一个项目，而是**按能力分层取长补短，重新组合成一个适配 iKuai 插件约束的用户态一体化引擎**。本轮已按该设计落地 `plugins/netfence`，可构建、可测试、可打包。

## 二、GitHub 调研结果（真实数据，2026-11-20 GitHub REST API 拉取）

原始 JSON：`.smoke-data/firewall_research_repos.json`。19 个候选中 17 个成功拉取，2 个 404（`ipfire/ipfire-2.x`、`openwrt/nlbwmon` 为路径变更，非项目不存在）。

| 项目 | ★ | 许可证 | 最近推送 | 语言 | 借鉴点 / 不采用原因 |
|---|---|---|---|---|---|
| pi-hole/pi-hole | 62137 | NOASSERTION | 2026-11-15 | Shell | DNS sinkhole 范式、按客户端分组；采用其「网络级拦截」思路 |
| mitmproxy/mitmproxy | 47103 | MIT | 2026-11-19 | Python | 本仓 runhooks 已整合，不重复 |
| GyulyVGC/sniffnet | 43154 | Apache-2.0 | 2026-11-20 | Rust | 流量画像 UI、按协议/国家维度；借鉴展示层 |
| AdguardTeam/AdGuardHome | 38622 | GPL-3.0 | 2026-11-20 | TypeScript | **规则格式与 DNS 过滤架构主参照**；GPL 不直接引入代码 |
| hagezi/dns-blocklists | 28997 | GPL-3.0 | 2026-11-20 | Text | 分级域名列表数据格式；作为可订阅上游 |
| TG-Twilight/AWAvenue-Ads-Rule | 6849 | GPL-3.0 | 2026-11-16 | 规则 | 中文广告规则上游，适配 AdGuard 格式 |
| chaitin/SafeLine | 23352 | GPL-3.0 | 2026-11-20 | Go | WAF/反向代理，方向是「保护自己的服务」，非网关管控，不采用 |
| crowdsecurity/crowdsec | 15711 | MIT | 2026-11-19 | Go | **协同封禁/威胁情报订阅范式主参照** |
| OISF/suricata | 6918 | GPL-2.0 | 2026-11-19 | C | IDS/IPS 规则与 IP 信誉；需内核抓包，本轮只借鉴数据结构 |
| pfsense/pfsense | 5794 | Apache-2.0 | 2026-03-31 | PHP | 规则表优先级语义；整机发行版，不移植 |
| opnsense/core | 4807 | BSD-2-Clause | 2026-11-20 | PHP | **MVC/API 策略分层、规则渲染与「先预览再下发」主参照** |
| ntop/nDPI | 4676 | LGPL-3.0 | 2026-11-20 | C | **L7 应用识别主参照**；依赖 libpcap，改用域名/SNI 特征匹配实现 80/20 |
| the-tcpdump-group/libpcap | 3188 | NOASSERTION | 2026-11-19 | C | 抓包基础库，插件不可用 |
| destan19/OpenAppFilter | 2963 | GPL-2.0 | 2026-11-11 | C | **上网行为管理/应用过滤主参照**；其 README 明确需 `oaf` 内核模块 + LuCI + daemon 三件套，且 GPL 衍生需保留署名，故只借鉴「可自定义特征库」设计 |
| e2guardian/e2guardian | 541 | GPL-2.0 | 2026-10-23 | HTML | 内容分级/短语过滤；正向代理，本轮不采用 |
| vyos/vyos | 41 | GPL-2.0 | 2017-01-16 | Python | 2.0 开发仓已停更，排除 |
| Rabbit-Company/BurrowGate | 8 | EUPL-1.2 | 2026-11-19 | TypeScript | 反向代理网关，星数低且与需求方向不符，排除 |

补充检索（Web）：`openappfilter` 官方页确认「基于流的 L7 识别、独立于 DNS、支持自定义协议特征」；阿里云社区文章列举 Panabit（协议识别 + 限速）、Xplico、Security Onion、PacketFence 等国内常见的上网行为管理/审计技术栈，与本设计的能力分层一致。

**许可证结论**：一律**不复制任何 GPL/LGPL 源码**，只借鉴设计思想与公开数据格式（AdGuard `||domain^`、hosts、CIDR 列表），插件代码独立实现，避免 GPL 传染与公司授权风险（OAF README 明确「公司使用需联系作者授权」）。

## 三、能力分层与「取长补短」映射

| 层 | 借鉴对象 | 取其优点 | 弃其短板 / 适配改造 |
|---|---|---|---|
| 域名过滤 | AdGuard Home / Pi-hole / hagezi / AWAvenue | 多格式规则、例外白名单、按客户端分组、订阅式列表 | 去掉整机 DNS 服务与 Web 框架；改为存储卷 `lists/*.txt` 纯文件解析，零依赖 |
| 应用识别 | nDPI / OpenAppFilter | L7 分类、可自定义特征库 | 放弃内核 DPI（NFQUEUE/kmod 在插件内不可用）；改用域名/SNI/Host 后缀特征表，覆盖主流应用，特征库可热更新 |
| 统一策略 | OPNsense (MVC/API) + 山石策略模型 | 主体 × 客体 × 时段 → 动作的单表、优先级、API 驱动 | 三套产品各管一段 → **一个 `Request` 类型、一张策略表**统一裁决 DNS/SNI/Host，裁决带 `layer/reason/rule_id` 溯源 |
| 防火墙规则 | OPNsense / nftables | 策略与内核规则分离、规则集可审阅 | 不做自动下发（避免误封断网）；`RenderACL` 纯函数渲染 nftables 文本供预览，输入做注入校验 |
| 行为审计 | nlbwmon / Sniffnet | 按设备统计、时间线、Top 应用 | 有界内存环，避免路由器 flash 被日志写满；持久化交给 service.sh |
| 威胁情报 | CrowdSec / Suricata | 协同封禁订阅、IP 信誉 | 引擎只读本地 `threat/*.txt`，刷新交给 busybox wget；断网不影响裁决 |

## 四、最优版架构（本轮已落地）

```
plugins/netfence/
├── metadata.json / install.sh / logo.png     # iKuai 插件契约
├── html/index.html                           # 一体化管控台(iframe 自带主题)
├── script/service.sh                         # start/stop/show/set_ports/reload/boot
├── script/default-policy.json                # 首装「只审计不拦截」安全默认
├── script/default-app-signatures.json        # 内置应用特征库(可扩展)
├── bins/netfence-x86_64                      # 源码侧静态引擎(CGO_ENABLED=0)
│                                             # metadata.bin 映射到 ipk 内 bin/netfenced
└── go/                                       # 纯标准库源码 + 单元测试
    ├── internal/core/    model/filterlist/appid/policy/acl/audit/threat/engine
    ├── internal/api/     localhost-only JSON API
    └── cmd/netfenced/    API + DNS sinkhole + SIGHUP 热重载
```

**六层裁决流水线**（`Engine.Decide`）：威胁情报 IP → 例外白名单 → 域名过滤 → 应用识别 → 统一策略表 → 默认放行。每个裁决都写入行为审计。

**关键设计取舍**

1. **一体化 = 一张策略表**：DNS qname、TLS SNI、HTTP Host 共用 `Request`/`Decision`，避免「防火墙一个策略、行为管理另一个策略」的割裂。
2. **首装不拦截**：默认策略是 `audit`，安装后不会断网 —— 直接针对山石类设备上线最常见的「策略配错全网卡死」事故。
3. **先预览再下发**：`/api/acl` 只渲染、`applied:false`；IP 封禁需运维确认后手工应用。
4. **热重载**：保存策略 / 改列表 → SIGHUP，不重启进程、不断流量（OPNsense API 驱动的优点）。
5. **静态可移植**：纯标准库 Go，`CGO_ENABLED=0`，符合固件 uClibc 门禁；`build.py` 的 `check_engine_version` 与 `validate_metadata` 双门禁通过。
6. **端口隔离**：管理口 127.0.0.1:18111（不与 runhooks 18060-62、18080-82 冲突），页面走 `/plugin_proxy/netfence/` 同源前缀，规避 CORS 与 HTTPS 混合内容。
7. **设备 jq 形态**：service.sh 全程 `printf`/`sed` 构造 JSON，不用 `--argjson`（该选项在设备裁剪版 jq 上恒返回空）。

## 五、诚实边界（本次未覆盖）

- **真机未验证**：本轮为构建机验证（go test / go vet / 静态构建 / PT_INTERP / build.py 打包 / 浏览器渲染），未做 iKuai 设备实机安装与 Web 管理页内 iframe 渲染。
- **无内核级 DPI/IPS**：L7 是域名/SNI/Host 特征匹配，不是 nDPI 那样的载荷级深度包检测；加密流量（ECH、QUIC 0-RTT）与任意端口应用的识别率有限。要补齐须固件侧配合 NFQUEUE + 内核模块，属固件仓库 `/opt/ikuai4-enterprise` 的工作，不是插件侧能独立完成的。
- **DNS 拦截默认关闭**：需在「服务与端口」启用并配合 nftables 把 LAN 53 重定向到 18110；本轮默认只启用管理 API。
- 未升线上版本、未提交、未 push、未打正式 ipk 发布（共享工作区版本链由其他成员推进，避免撞车）。


## 附：v1.0.1 真机 P1 修复（2026-11-20）

测试报告 `TEST-REPORT-2026-11-20-netfence-v1.0.0-real-device-verification.md` 指出：
框架经 `/usr/ikuai/function/plugin_netfence` 软链调用 `service.sh`，而 v1.0.0 用
`dirname "$0"` 定位安装目录，导致 `BIN` 指向不存在的路径，启动返回 1、
`show TYPE=data` 版本报 `unknown`、按钮状态不变。

v1.0.1 修复：
1. 照同仓库 runhooks 的 `[self-locate]` 段补 `readlink` 软链解析，并对二进制位置
   做多候选探测（不同固件传给脚本的 `$0` 形态不完全一致）；失败原因回显到 stdout。
2. 补齐固件 `plugin_proxy` 端口文件 `/tmp/iktmp/plugin_web/netfence` 的写入/清理，
   消除引擎起来后页面 API 仍 502 的连坐问题；保留 `BUILD_DATE` 自适应绑定地址。
3. `wget` 改用 busybox 兼容的 `-T2`；`install.sh` 补显式 `return 0`（坑 89）与
   autostart 开机自启；`start()` 改直接后台拉起，不依赖 `pgrep`/`start-stop-daemon`。
4. 回归自测 `tests/p1-symlink-selftest.sh`：在临时目录搭真机同构目录树、经软链调用，
   断言 BIN/`__bin_check`/版本/端口文件/API 可达/stop 清理 7 项；对 v1.0.0 原脚本
   能完整复现三症状、对 v1.0.1 全绿。

## 附：v1.1.0 管控落到真实流量 + 配置面补齐 + 布局优化（2026-11-21）

探索性 QA 报告 `TEST-REPORT-2026-11-21-netfence-exploratory-firewall-qa.md`
给出 P0「当前更像策略预演 + 审计原型，不是可用防火墙」及 11 条缺陷。v1.1.0 逐项销账：

**P0 管控落到真实流量**
- D1 `service.sh` 在 `dnsEnable=1` 时下发 `NETFENCE_DNS` 专用 iptables 链，把
  LAN udp/tcp 53 重定向到 `DNS_PORT`；排除路由器自身 IP、上游 DNS、引擎自身端口
  防回环；`stop` 整链撤销；`show` 回传 `dnsRedirect` 状态。回归
  `tests/d1-dns-redirect-selftest.sh`（桩替 iptables，12 项断言）。
- D2 `/api/acl` 仍只渲染（`applied:false`），下发独立为 `/api/acl/apply`，
  必须 `confirm=true` 二次确认；`nft -f -` 只接受 `table inet` 规则集，
  撤销按自有表名整表删除，不碰他人规则。
- D8 DNS 应答里的 A/AAAA 解析结果过一遍威胁情报 L3 复核（`DecideResolvedIP`），
  旧版 `handleDNS` 不带 `DstIP`，威胁 IP 层在数据面不可达。

**P1 配置面缺失 + 假成功**
- D3/D4 `/api/lists`、`/api/threat` 补齐 POST 写入 / DELETE 删除，带文件名校验
  （防目录穿越）与原子落盘，写入后 `Reload` 立即生效；页面补上传/编辑入口。
- D5 `service.sh` 补 `feeds.conf` 订阅下载（busybox wget）与 `refresh_feeds`。
- D10 只读端点对未实现的 method 返回 405 + `Allow` 头，不再「200 但内容不变」。

**P2/P3 数据可信度与逻辑**
- D6 `/api/decide` 服务端强制 `DryRun`，预演写 `source=probe` 且不落行为审计；
  `/api/audit` 支持按 `source` 过滤与 DELETE 清空。
- D7 时段策略统一用 `ScheduleTZ`（东八区）比较，不再按设备 UTC 裸时钟差 8 小时。
- D9 DNS 增 TCP/53（2 字节长度前缀）与并发上限；上游失败回 SERVFAIL 而非静默空应答。
- D11 `PUT /api/policy` 带 `X-Policy-Rev` 做乐观并发检查，落后返回 409。

验证：`go test ./...` 3 包全绿（31 用例，含 P0 回归）、`go vet` 0；真实引擎冒烟
（编译产物起 `-api/-dns/-upstream`，发真实 UDP/TCP DNS 与 HTTP）15 项全过；
外壳页布局参考巡流 NetLoom 做 CSS-only 优化，浏览器 A/B 23 项断言全过（该布局步骤
内 DOM/绑定/文案逐字未动；相对 v1.0.1 的整体 html 差异另含上面的配置面 UI 新增）。默认策略仍 `default-audit` 只审计不拦截，安装不断网。


## 附：v1.1.2 引擎优化与发布（2026-11-25）

本版为引擎侧优化与安全加固，不改裁决语义：

- `FilterList.Lookup` 由 `strings.Split` + 逐层 `strings.Join` 改为按字节索引逐级切后缀，
  DNS 热路径零分配（基准 `BenchmarkLookup_Suffix`: 0 B/op、0 allocs/op）。
- `FilterList.ParseList` 由逐行加解锁改为整段一次持锁；wildcard 用集合去重，
  大列表加载不再退化为平方级。
- 管理 API 统一加请求体上限（lists/threat 4MB，其余 1MB）；空或全注释列表在落盘前拒绝，
  不再清空过滤规则；`http.Server` 补齐读写超时；DNS over TCP 的 `Accept` 临时错误退避重试。
- 新增 `optimize_regression_test.go`（core/api 各一份）覆盖后缀/例外/wildcard 语义、
  零分配、去重、并发安全、超大 body、空列表与 threat CIDR 写入。

自测：`go test ./...`、`go vet ./...`、`go test -race ./...` 全通过。
发布件 `plugin-netfence-x86_64-v1.1.2-Build<ts>.ipk` 落在 server 工作区 `/opt/ikuai4/`。


## 附：v1.1.3 页面视觉层重做（2026-11-26）

- 去掉说明条的单行省略与悬停展开交互：提示与说明直接完整呈现，无角标、无展开动作。
- 布局重做：分区标题行、五等宽 KPI 网格、八等宽分段 tab、卡片化表格与说明块、
  统一间距刻度、三档响应式（1180/820），取消浮起阴影与位移。
- **CSS-only**：DOM 结构、Vue 绑定、文案、后端动作逐字未动
  （`@click` 28、`v-model` 12、`v-show` 8、`v-if` 8、`v-for` 2、插值 32 全部一致）。
- 自测：Chromium headless 真渲染截图（1280×1000，72KB），HTMLParser 解析通过、
  CSS 括号平衡、hover 展开规则 0；裁剪放大 OCR 复核说明全文完整显示，
  KPI 数值、管控台、8 个 tab、表头全部渲染。
- 发布件 `plugin-netfence-x86_64-v1.1.3-Build<ts>.ipk` 落在 server 工作区 `/opt/ikuai4/`。
