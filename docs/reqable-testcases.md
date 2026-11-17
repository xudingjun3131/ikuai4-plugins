# 小黄鸟抓包（reqable 插件）测试用例文档

> 用途：交给测试方（另一个 agent）逐条执行验证。覆盖本插件**全部已实现功能**与**历史实踩问题**的修复回归。
> 被测对象：iKuai 企业版固件上的 `reqable` 插件（引擎 `/opt/reqable`，Go 全静态二进制 + 内置 WebUI）。
> 文档版本：2026-11-17 · 对应插件版本 **≥ 1.0.38**

---

## 0. 测试环境与准备

| 项 | 值 |
|---|---|
| 路由器 | `192.168.9.1`（iKuai 企业版，admin 登录；**Web 登录凭证向本说明的使用者索取**，勿写进报告外泄） |
| 插件页（router 侧） | `https://192.168.9.1/` → 扩展服务 → 插件管理 → 小黄鸟抓包 |
| 引擎 WebUI | `http://192.168.9.1:18081/` |
| 引擎 API 基址 | `http://192.168.9.1:18081/api/`（下文 `$A`） |
| 显式代理端口 | `18080`（下文 `$P`） |
| 透明代理端口 | `18082` |
| 插件版本确认 | 插件页看版本号；或 `curl -s $A/api/health` |
| 引擎 data-dir | `/etc/disk_user/plugins/.reqable_data`（在数据卷；`rules.json`/`scripts/`/`ca.pem`/`geoip.csv` 均在此） |

**必备工具：回显服务器**（验证"改包再转发"唯一可靠的手段——必须看上游实际收到了什么）：

```bash
# 在测试机(记为 $TESTIP，如 10.10.10.7)执行, 监听 19130
python3 - <<'EOF'
import http.server, json
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({"path": self.path,
                           "headers_seen_by_upstream": dict(self.headers.items())}).encode()
        self.send_response(200); self.send_header("Content-Type","application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("0.0.0.0", 19130), H).serve_forever()
EOF
```

**通用手法**：任何"规则是否生效"的争议，都用 `curl -x $P http://$TESTIP:19130/xxx` 让请求过代理，看回显里 `headers_seen_by_upstream` / 响应体，而不是看感觉。

**通过标准**：每条用例的「预期结果」全部满足记为 PASS，否则 FAIL 并在报告里附实际输出。

---

## 1. 引擎基础（TC-01 ~ TC-04）

### TC-01 引擎启动 / 停止 / 端口监听
- **步骤**：插件页点「启动引擎」（可勾开机自启）→ 等 3 秒 → `ss -tlnp | grep -E '1808[0-2]'`（或 `curl -s -o /dev/null -w "%{http_code}" $A/api/health`）→ 点「停止引擎」→ 再查监听
- **预期**：启动后状态徽标「运行中」，18080/18081/18082 三个端口均有监听，`/api/health` 返回 `{"ok":true,...}`；停止后徽标「已停止」，端口全部释放
- **结果**：□ PASS □ FAIL　实际：

### TC-02 抓包总开关必须真停（用户实踩：**"点了关闭抓包，proxy 还在抓、WebUI 流量还在涨"**，v1.0.38 修复）
- **步骤**：
  1. 插件页点「开启抓包」→ `for i in 1 2 3; do curl -s -x $P http://$TESTIP:19130/on$i >/dev/null; done` → `curl -s $A/api/flows | jq '.flows|length'` 记 `N1`
  2. 插件页点「关闭抓包」→ `for i in 1 2 3 4 5; do curl -s -x $P http://$TESTIP:19130/off$i >/dev/null; done` → 再取 flow 数 `N2`
  3. 看页面抓包徽标文字
- **预期**：`N2 == N1`（**关键：关了对显式代理设备也不再记录**，以前只撤 iptables 不停引擎记录层）；徽标显示「抓包已关」；`curl -s $A/api/capture` 返回 `{"paused":true}`
- **结果**：□ PASS □ FAIL　实际：

### TC-03 开关状态与引擎启动对齐（防"关了抓包再重启引擎又静默开始记录"）
- **步骤**：保持 TC-02 第 2 步的关闭状态 → 插件页「停止引擎」→「启动引擎」→ 发 3 个请求过代理 → 看 flow 数
- **预期**：引擎起来后 `curl -s $A/api/capture` 为 `{"paused":true}`；3 个请求后 flow 数**不涨**
- **结果**：□ PASS □ FAIL　实际：

### TC-04 抓包状态三态显示
- **步骤**：a) 开关关着看徽标；b) 开开启抓包看徽标；c) 在**引擎 WebUI** 顶栏点「暂停」→ 刷新插件页看徽标
- **预期**：三态分别为「抓包已关」/「抓包中」/「已暂停(引擎)」；WebUI 徽标对应「已暂停」且按钮变「继续」
- **结果**：□ PASS □ FAIL　实际：

---

## 2. 规则系统（TC-05 ~ TC-08）

### TC-05 规则面板完整编辑器（用户实踩：**"规则面板没看到怎么改"**，v1.0.36 重做）
对以下 **12 种类型各建 1 条**（点「新建」→ 选类型 → 填字段 → 保存），验证：字段表单随类型变化、保存后列表出现、摘要正确、可停用/启用、可编辑、可删除：

| 类型 | 必填字段 | 快速验证法 |
|---|---|---|
| 内容替换 body_replace | 阶段(响应/请求)、查找、替换为 | 见 TC-07 |
| 头改写 header | 阶段、设置(键值对,可加多行)、删除(逗号分隔) | 见 TC-07 |
| 本地文件映射 map_local | 文件(相对 map-local/)、状态码 | 命中后返回文件内容 |
| 镜像远端 mirror_remote | URL | 命中后返回该 URL 的内容 |
| 上游网关 gateway | upstream | 命中后请求去上游 |
| 二级代理 secondary_proxy | proxy_url | 命中后走二级代理 |
| 反向代理 reverse_proxy | listen、target | 监听端口通 |
| 拦截域名 host_block | 无 | 命中 403 |
| 放行域名 host_allow | 无 | 存在时任一条命中即放行,其余 403 |
| 访问控制 access_control | CIDR、动作 | 按客户端 IP 放行/拒绝 |
| 不解密 ssl_bypass | 无 | 该 host 不走 MITM |
| 高亮标记 highlight | 颜色、标签 | 命中流量带色+标签 |

- **附加验证**：匹配条件三项——域名支持 `*.example.com` 通配、路径为**正则**（填非法正则在保存前被拒并报错）、方法逗号分隔；列表每条有类型徽标+摘要+启停开关+编辑+删除
- **预期**：12 类型全部可建可改可停可删；正则非法有明确报错；匹配条件语义如上
- **结果**：□ PASS □ FAIL　实际：

### TC-06 内置规则（用户实踩：**"规则里面是空的，加一些内置规则"**，v1.0.37）
- **步骤**：
  1. 删空所有规则 → 插件页/引擎重启引擎 → 看规则列表
  2. 规则面板点「内置」按钮（有确认框）→ 看列表
  3. `curl -s -X PUT $A/api/rules | jq '{loaded, n:(.rules|length)}'`
  4. 核对 10 条：`builtin-no-cache`/`builtin-accept-encoding`/`builtin-cors`/`builtin-strip-csp`/`builtin-hl-api`/`builtin-hl-telemetry-{clarity,ga,gtm,dc}`/`builtin-hl-static`
- **预期**：① 删空后重启**不会**被复活（用户删掉的规则不被开机重置，是设计行为）② 点「内置」后 10 条全部回来且**全部启用**，带「内置」角标 ③ PUT 接口同样返回 `loaded:"builtin"` 且 10 条
- **结果**：□ PASS □ FAIL　实际：

### TC-07 改包再转发（核心需求：**"怎么修改这个包的内容在转发到客户端"**）
- **步骤**：
  1. 启用 `builtin-accept-encoding`（开关打开）
  2. 新建「内容替换」规则：匹配 `path: ^/api/`，阶段=响应，查找 `bird`，替换为 `BIRD`
  3. `curl -s -x $P http://$TESTIP:19130/api/saybird`（回显需在响应体里含 `bird`，可用下方脚本）
  4. 同时看：回显的 `headers_seen_by_upstream.Accept-Encoding`、客户端收到的响应体、引擎 WebUI 里该流的 Headers 标签页
- **预期**：上游收到 `Accept-Encoding: identity`；**客户端收到的响应体里 `bird` 已变成 `BIRD`**（改完后转发）；WebUI 的 Headers 页显示的就是改写后的头（含规则加的头）
- **结果**：□ PASS □ FAIL　实际：

> 回显服务器含 body 的版本（替换上面第 0 节的 `do_GET`）：
> ```python
> body = json.dumps({"say":"bird","path":self.path,"headers":dict(self.headers.items())}).encode()
> ```

### TC-08 压缩响应边界（已知约束，验证"坑"确实存在且解法有效）
- **步骤**：a) 停用 `builtin-accept-encoding`，对返回 gzip 的上游做 TC-07 第 3 步；b) 再启用它重做
- **预期**：a) 明文替换**不生效**（hook 层拿到的是压缩字节，`display.go` 只为**显示**解压——这不是 bug 是边界）b) 启用 identity 后替换生效。测试方应把 a) 记为「符合预期」而非缺陷
- **结果**：□ PASS □ FAIL　实际：

---

## 3. 脚本模板（TC-09 ~ TC-10）

### TC-09 QuantumultX-Rewrite 转换模板（需求：**"把 QX-Rewrite 的规则加入到模板"**，v1.0.36）
- **步骤**：引擎 WebUI → 脚本页 → 看模板列表 → 逐个「应用」以下 10 个 → 每应用一个，`curl -s $A/api/scripts` 确认安装 → 用匹配 URL 过代理验证效果（至少验证 3 个）：
  `qx-cubox`、`qx-calculator-hd`、`qx-wallcraft`、`qx-wps`、`qx-audiomack`、`qx-bedtimefan`、`qx-emby`、`qx-dayone`、`qx-grammarly`、`qx-tokcam`
- **验证样例（cubox）**：应用 `qx-cubox` 后 `curl -s -x $P http://$TESTIP:19130/c/api/userInfo`（回显返回 `{"data":{"level":0,...}}`）→ 响应体应变 `level:1`、`nickName:"Cubox VIP"`、带 `expireTime`
- **守卫验证**：请求**不含** `/c/api/userInfo` 的路径 → 响应体**原样不动**
- **预期**：模板列表共 13 个（10 QX + 3 原有）；应用即装即用；命中改写、未命中原样
- **结果**：□ PASS □ FAIL　实际：

### TC-10 原有 3 个模板死代码修复（顺修：add-header/strip-cookie/log-url 以前从未生效）
- **步骤**：分别应用并验证：
  - `add-header` → 过代理任意请求 → 上游收到 `X-Proxying-Script: 1`
  - `strip-cookie` → `curl -x $P -H "Cookie: a=1" ...` → 上游**收不到** Cookie
  - `log-url` → 过代理请求 → 响应头出现 `X-Captured-Path: <请求URL>`
- **预期**：三个都真实生效（修复前它们是顶层代码没包函数，runner 的 `AssertFunction` 取不到函数，等于死代码）
- **结果**：□ PASS □ FAIL　实际：

---

## 4. 抓包控制与设备（TC-11 ~ TC-14）

### TC-11 断点拦截（breakpoint，交互式改包）
- **步骤**：
  1. `curl -s -X PUT $A/api/breakpoints -H 'Content-Type: application/json' -d '{"enabled":true,"match":{"host":"*"}}'`
  2. 后台发请求：`(timeout 40 curl -s -x $P http://$TESTIP:19130/bp-test > /tmp/bp_client.txt 2>&1 &)`
  3. 3 秒后 `curl -s $A/api/breakpoints | jq '.sessions'` —— 应出现 **phase=request** 的会话（**注意：host 为 `*` 时请求和响应两个阶段都会挂，一个流会先后产生两个会话，每个都要单独 resume，客户端只有响应阶段放行后才收到内容**）
  4. 放行请求阶段（可带头编辑）：`curl -s -X POST $A/api/breakpoints/<req_id>/resume -d '{"headers":{"X-BP":"yes"}}'`
  5. 再取会话列表 → 出现 **phase=response** → 放行并改响应体：`curl -s -X POST $A/api/breakpoints/<resp_id>/resume -d '{"body":"{\"hooked\":true}"}'`
  6. `cat /tmp/bp_client.txt`
- **预期**：开启后请求被挂起（前台 curl 不返回是正常现象）；两个会话都能在列表看到；resume 带头/带 body 后**客户端最终收到改后的响应体** `{"hooked":true}`；`match.host` 为 `*` 时必须挂住任何 host（2026-11-17 修复：breakpoint 的 hostMatch 漏了 `*` 特判，"全匹配"断点此前从来没反应）
- **结果**：□ PASS □ FAIL　实际：

### TC-12 抓包设备选择（透明模式）
- **步骤**：插件页「抓包设备」勾选某台设备 IP → 开启抓包 → 看 `iptables -t nat -S REQABLE_REDIRECT`（设备上无 SSH 时看插件页「网络诊断」）→ 该设备上网是否走代理；取消勾选 → 规则撤销
- **预期**：勾选后只有该设备流量被 REDIRECT 到 18082；**不选任何设备 = 不抓任何设备（绝不全局）**；取消后规则撤
- **结果**：□ PASS □ FAIL　实际：

### TC-13 记录层范围（scope）
- **步骤**：插件页设置范围（如 only + 路径 `/api/`）→ 开启抓包 → 分别请求匹配/不匹配路径 → 看 flow 列表
- **预期**：只有匹配的落库；MITM 已发生只是不落库；`only` 且条件为空 = 全绕过（不抓任何）
- **结果**：□ PASS □ FAIL　实际：

### TC-14 CA 证书
- **步骤**：a) 插件页点「下载 CA 证书」；b) 引擎 WebUI 点「CA证书」；c) `curl -s $A/api/ca` 看 base64
- **预期**：三条路都能拿到 CA（router 页走同源 base64 通道，不被混合内容拦）；安装并信任后 HTTPS 解密正常（能看到明文请求/响应）
- **结果**：□ PASS □ FAIL　实际：

---

## 5. 配置与数据（TC-15 ~ TC-18）

### TC-15 端口配置
- **步骤**：插件页把显式代理端口改成 19090 → 保存 → 看是否自动重启引擎 → `curl -x $P http://$TESTIP:19130/port-test`（$P 用新端口）→ 改回 18080
- **预期**：改端口自动重启引擎，新端口立即可用；页面提示以本页端口为准（引擎界面里显示的 proxy 地址是写死默认值，**不是 bug**）
- **结果**：□ PASS □ FAIL　实际：

### TC-16 数据目录与 sda3 零数据（用户硬性要求：**"不要放在 /sda3 下面"**）
- **步骤**：`ls /etc/disk_user/plugins/.reqable_data/`（应有 rules.json/scripts/ca.pem/geoip.csv）→ `df -h /etc/mnt` → 抓一批包后再 `df -h /etc/mnt`
- **预期**：引擎数据全在数据卷；`/etc/mnt`（sda3，43MB）占用不随抓包增长；`/etc/mnt` 下无 `plugins` 目录
- **结果**：□ PASS □ FAIL　实际：

### TC-17 定时抓包
- **步骤**：设置一个**当前不在**的时段 → 点「开启抓包」→ 看提示与 `iptables` 规则 → 把时段改成当前 → 等到下个调度周期
- **预期**：不在时段内开启时提示"到点自动生效"且规则不上；到点后规则自动上
- **结果**：□ PASS □ FAIL　实际：

### TC-18 暂停 / 清空 / HAR 导出 / 流量计数
- **步骤**：WebUI 点「暂停」→ 发请求 → 列表不涨、徽标「已暂停」；点「继续」→ 恢复；点「清空」→ 列表空；点「HAR导出」→ 下载 .har 能打开；顶栏条数=列表长度
- **预期**：四项全部正常；**暂停后旧流量仍留在列表里可查看是设计行为**（暂停只停记录，不停查询）
- **结果**：□ PASS □ FAIL　实际：

---

## 6. 已知边界（**不是缺陷**，测试方勿报错）

1. **压缩响应做明文替换不生效**：hook 层拿原始压缩字节，`display.go` 只为显示解压。解法=启用 `builtin-accept-encoding`（或自建请求头规则设 `Accept-Encoding: identity`）
2. **规则不匹配就不生效**：域名通配只支持 `*.域名` 前缀形式（`*keyword*` 这种中间通配不支持）；路径是正则；方法空=任意。匹配不上时请先核对摘要行的域名/路径/方法
3. **删空规则后重启不会被内置规则复活**：种子只在 rules.json **不存在**时写；找回来用「内置」按钮
4. **暂停后旧流量仍在列表**：暂停只停 `Add`，查询/清空不受影响
5. **引擎 WebUI 里显示的 proxy 地址是写死默认值**：以插件页端口为准
6. **>6MB 的 ipk 不能走页面上传**（rootfs 是 RAM 盘）：在线安装
7. **显式代理设备不受 iptables 规则影响**：所以"关闭抓包"必须同时停引擎记录层（TC-02 正是验证这个）

---

## 7. 签收表

| 用例 | 标题 | 优先级 | 结果 | 备注 |
|---|---|---|---|---|
| TC-01 | 引擎启停与端口 | P0 | □P □F | |
| TC-02 | **关闭抓包必须真停** | **P0** | □P □F | |
| TC-03 | 开关与启动对齐 | P0 | □P □F | |
| TC-04 | 三态显示 | P1 | □P □F | |
| TC-05 | 规则编辑器 12 类型 | P0 | □P □F | |
| TC-06 | 内置规则 10 条 | P0 | □P □F | |
| TC-07 | **改包再转发** | **P0** | □P □F | |
| TC-08 | 压缩响应边界 | P1 | □P □F | |
| TC-09 | QX 转换模板 10 个 | P0 | □P □F | |
| TC-10 | 原 3 模板修复 | P1 | □P □F | |
| TC-11 | 断点拦截 | P1 | □P □F | |
| TC-12 | 设备选择 | P0 | □P □F | |
| TC-13 | scope 记录层 | P1 | □P □F | |
| TC-14 | CA 证书 | P0 | □P □F | |
| TC-15 | 端口配置 | P1 | □P □F | |
| TC-16 | 数据目录/sda3 | P0 | □P □F | |
| TC-17 | 定时抓包 | P2 | □P □F | |
| TC-18 | 暂停/清空/HAR | P1 | □P □F | |

**测试方信息**：执行人\_\_\_\_\_\_\_\_　日期\_\_\_\_\_\_\_\_　插件版本\_\_\_\_\_\_\_\_　固件 Build\_\_\_\_\_\_\_\_\_

**FAIL 用例请附**：步骤序号、实际输出（curl 原文/截图）、引擎日志尾部（`/etc/log/plugins/logs/` 或 data-dir 下 reqable 日志）。
