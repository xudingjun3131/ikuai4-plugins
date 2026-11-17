# Mitmproxy 插件测试用例（v1.0.2）

> 设备：`192.168.9.1`（iKuai 企业版，admin 登录；凭证向本说明的使用者索取）
> 插件页：`https://192.168.9.1/` → 扩展服务 → 插件管理 → Mitmproxy抓包
> mitmweb：`http://192.168.9.1:18071/`（端口可配）
> 架构：regular@18070（显式）+ transparent@18072（iptables REDIRECT）+ web@18071，三端口都可配
> 前置：插件已在线安装（ipk 约 31MB >6MB，**只能在线安装**，页面上传必失败）

## 本地已验证（构建机，2026-11-17，无需设备）

| # | 项 | 方法 | 结果 |
|---|----|------|------|
| L-1 | 双模式并存启动 | `run-mitmweb.sh --mode regular@127.0.0.1:29070 --mode transparent@0.0.0.0:29072` | 三端口全 LISTEN，显式代理 200，flow 落库 |
| L-2 | 透明 HTTP | netns 模拟 LAN 设备（10.99.0.2）+ PREROUTING REDIRECT 80→透明口 | **200**，flow `sockname=原始目标:80`（SO_ORIGINAL_DST 正确），`peername=客户端真实IP` |
| L-3 | 透明 HTTPS | 同上 REDIRECT 443 | **200**，flow `tls_established=True`（TLS 拦截通） |
| L-4 | netdiag JSON 合法 | 含引号/TAB/CR 的日志 + 多行 devices.json 过 `__show_netdiag` 同款管道 | JSON 合法（reqable 的裸换行事故不重犯） |
| L-5 | 端口解析/校验 | 13 条单测（默认值/旧两字段兼容/三字段/相同端口/非数字/越界/空值/dports 正则） | 13/13 PASS |

## 设备待验用例（需先 push 发布并升级到 1.0.2）

### TC-01 引擎启停与三端口监听 — 待验
- 页面点「启动引擎」→ 3~5s 内 status=1；`ss -tln`（或页面网络诊断）应见 18070/18071/18072 三个 LISTEN。
- 点「停止引擎」→ status=0，三端口释放，**iptables 规则同时撤除**（见 TC-07）。

### TC-02 显式代理抓包 — 待验
- `curl -x http://192.168.9.1:18070 http://ifconfig.me/ip` → 200，回显设备公网 IP。
- HTTPS：`curl -kx -x http://192.168.9.1:18070 https://www.baidu.com` → 200（-k 因未信任 CA）。
- mitmweb `/flows` 条数增加。

### TC-03 透明抓包 HTTP（核心新功能）— 待验
- 页面勾「抓包总开关」→ 设备列表勾选测试机（如 192.168.9.100/pc）→ 保存（自动）。
- 测试机**不设任何代理**，浏览器开 `http://ifconfig.me/ip` → 页面正常打开（不被断网）。
- mitmweb `/flows` 出现该请求，`client_conn.peername` = 测试机 IP，`sockname` = 真实目标 IP:80。
- 判据：透明流量与显式流量在 mitmweb 里都能看到，且页面标签「透明规则生效」。

### TC-04 透明抓包 HTTPS — 待验
- 同上环境，测试机访问 `https://www.baidu.com`：
  - **未装 CA**：浏览器证书错误（预期，属已知边界）；
  - **装了并信任 CA**：页面正常，mitmweb 看到解密后的请求/响应（`tls_established=True`）。
- QUIC 阻断：选中设备的 UDP443 被 DROP，浏览器回退 TCP（日志/规则可见 MITMPROXY_QUIC）。

### TC-05 设备选择精确性 — 待验
- 只勾 1 台设备：另一台未勾选设备上网**完全不受影响**（不断网、不跳证书错误）。
- 路由器自身访问 192.168.9.1 管理页正常（规则对路由器全部 IP RETURN）。
- 取消勾选全部设备 → 规则清空（rulesOn=0），网络恢复原样。

### TC-06 抓包总开关 — 待验
- 开：引擎没跑会自动拉起（不会出现"规则指向没监听的端口把设备 80/443 弄断"）。
- 关：iptables 规则撤除，设备上网立即恢复；显式代理流量此时**仍会**进引擎（显式代理不受 iptables 管，属已知边界，与 reqable 同）。

### TC-07 停止引擎自动撤规则 — 待验
- 开着抓包+勾了设备，直接点「停止引擎」→ 规则撤除（netdiag 显示 rules_flag=off），设备不断网。
- 重启引擎（点启动）→ 规则自动重建（boot-heal，netdiag rules_flag=on）。

### TC-08 端口配置 — 待验
- 显式 18070→19070 保存：引擎自动重启，19070 OPEN、18070 CLOSED，`curl -x :19070` 通。
- 透明 18072→19072 保存：iptables REDIRECT 目标跟着变（netdiag 里 `--to-ports 19072`），透明抓包仍通。
- 三个端口填相同值/非数字/越界 → 明确报错，不落盘。

### TC-09 拦截端口配置 — 待证
- 「拦截端口」改 `80,443,8080` 保存 → netdiag 的 multiport 规则变 `--dports 80,443,8080`。
- 填非法值（如 `80,http`）→ 报错不落盘。

### TC-10 CA 证书 — 待验
- 页面「下载 CA 证书」→ 得到 PEM（约 1.5KB，BEGIN/END CERTIFICATE 齐全）。
- `show TYPE=ca` 返回 `ca_b64` 非空；引擎未启动时返回空且页面提示"先启动引擎"。
- CA 落在存储卷 confdir（`$EXT_PLUGIN_CONFIG_DIR/mitmproxy/`），**不在 sda3**。

### TC-11 清空流量 — 待验
- 点「清空流量」→ `/flows` 归零（随后新流量正常入账）。

### TC-12 网络诊断 — 待验
- 点「网络诊断」→ 面板渲染出 nat/filter 规则、设备表、日志尾部（**JSON 必须能解析**——reqable 曾因裸换行整个面板打不开，本版本已防）。
- 勾选设备后诊断里应看到：`-A MITMPROXY_REDIRECT -p tcp -m multiport --dports 80,443 -m mac --mac-source <MAC> -j REDIRECT --to-ports 18072`、路由器 IP 的 RETURN、`PREROUTING -j MITMPROXY_REDIRECT` 跳转。

### TC-13 开机自启与重启自愈 — 待验
- 勾「开机自启」→ 重启路由器 → 引擎自动起、三端口在、若总开关开着则规则自动重建。
- 不勾自启 → 重启后引擎不起（尊重用户状态）。

### TC-14 存储与 sda3 — 待验
- confdir（CA/配置/pid/devices.json）落在存储卷；sda3（/etc/mnt）零增长（df 对比）。
- 改存储路径后重启：自定位逻辑生效，不报 "No such file or directory"。

### TC-15 升级链路 — 待验
- 升级到 1.0.2 后**引擎自动重启**（install.sh 的 stale-proc 修复）→ 新功能（透明端口/设备页）立即可用，不用重启路由器。
- 升级后 `show TYPE=data` 的 version 相关字段与页面显示一致。

### TC-16 与 reqable 插件并存 — 待验
- 两个抓包插件同时启用：链路名不同（MITMPROXY_* vs REQABLE_*），互不干扰；各自开关独立。
- 同时勾选同一设备时行为：后建的规则在链顶，实际以后建者为准（已知边界，文档明示用户不要同时开两个透明抓包）。

## 已知边界（不是缺陷）

1. **显式代理流量不受总开关/设备选择约束**——手动设了代理的设备流量照样进引擎（iptables 管不到显式代理）。要"全停"需客户端取消代理设置。reqable 同款边界。
2. **未信任 CA 的域名 HTTPS 握手失败**——MITM 代理的固有现象，页面已提示装 CA。
3. **mitmweb 无"暂停记录"能力**（reqable 引擎有）——想停只能关总开关（撤规则）或停引擎。
4. **两个抓包插件同时透明引流同一设备会互相抢流量**——链路独立但 REDIRECT 目标不同，实际以后建规则为准；建议只开一个。
5. **QUIC/HTTP3 被阻断是故意的**——不阻断就绕过 MITM，浏览器自动回退 TCP。
6. **透明模式只处理 TCP**——UDP 非 443 端口不拦截。
7. **启动约 3~5 秒**——Python 运行时初始化慢，start() 最多等 15s。

## 签收表

| 用例 | 结果 | 验证人 | 日期 | 备注 |
|------|------|--------|------|------|
| L-1~L-5 本地 | PASS | 运维开发 | 2026-11-17 | 构建机 netns 实测 |
| TC-01 | | | | |
| TC-02 | | | | |
| TC-03 | | | | |
| TC-04 | | | | |
| TC-05 | | | | |
| TC-06 | | | | |
| TC-07 | | | | |
| TC-08 | | | | |
| TC-09 | | | | |
| TC-10 | | | | |
| TC-11 | | | | |
| TC-12 | | | | |
| TC-13 | | | | |
| TC-14 | | | | |
| TC-15 | | | | |
| TC-16 | | | | |
