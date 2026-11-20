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
├── bins/netfence-x86_64                      # CGO_ENABLED=0 静态引擎
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
