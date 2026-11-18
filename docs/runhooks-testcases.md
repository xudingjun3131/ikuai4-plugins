# runhooks 插件测试用例(v1.0.0)

> 引擎源码: `/opt/runhooks-engine`(Go, fork 自 reqable 引擎, 模块 github.com/GitterBois/runhooks)
> 插件源码: `/opt/ikuai4-plugins/plugins/runhooks`
> 端口分配: 18060 显式代理 / 18061 API+WebUI / 18062 透明监听(与 reqable 18080-18082、mitmproxy 18070-18072 不冲突)

## 一、单元测试(构建机, 全绿)

```bash
cd /opt/runhooks-engine && go test ./internal/...
```

| 用例 | 断言 |
|---|---|
| TestDetectFormat | QuanX/Surge/纯列表 格式识别(靠独有段名, [general]/[mitm] 不参与判别) |
| TestParseQuanX | [mitm] hostname、[rewrite] 全动作(request-header/response-header/echo-response/reject/redirect/url/script-*)、[filter_local] 脚本与 reject、[host]/[server_local] 忽略 |
| TestParseQuanXRules | 8 类支持规则 + no-resolve; DOMAIN-SET/IP-ASN/FINAL 进 skipped+warnings |
| TestParseSurge | [Rule]/[MITM](%APPEND% 剥除, %.x 保留)/[Script](type→kind, script-path→URL)/Map Local 跳过 |
| TestParseList | hosts 行/||x^ 形态/IP/CIDR; 段外 TYPE,VALUE 规则行混排识别 |
| TestHostFromPattern | 正则转义(\. 字面量)、(www\.)? 可选分组跳过、.* 通配、纯主机正则返回空 |
| TestAdaptDomains | %. → *.、REJECT→ssl_bypass、DOMAIN-KEYWORD→*kw*、URL-REGEX→^/path、域名与 IP/GEOIP 冲突时丢弃并 warning |
| TestAdaptIPOnly | 纯 IP 订阅: dst_ips + countries 白名单生效 |
| TestAdaptRewrites | header(双相位)/body_replace(From 空=整包替换)/map_local 内联 body/host_block/mirror_remote(30x 降级)/gateway |
| TestAdaptScripts | 脚本转换 + 未取到内容的 warning |
| TestConvertQXScript* | $response.body 映射、$done({body})/Surge $done({response:{body}})/空 $done、request 相位、$notify/$prefs 打桩、残留 $ 变量告警、无 pattern 不加守卫 |
| TestSubRulesSeparation | rules.json/rules.sub.json 物理分离: Save 不落订阅规则、SaveSubRules 只写订阅层、WebUI 整包保存不污染、清空订阅层 |
| TestScopeSubMerge* | 订阅域名→only 白名单、用户 only 配置取并集、记录层 paths 并集+切 only、纯 IP 订阅、中间通配 *kw* |

## 二、订阅链路端到端冒烟(构建机, 回环)

```bash
cd /opt/runhooks-engine && bash scripts/smoke_subscription.sh
```

本地 http.server 作订阅源(QuanX conf + unlock.js), 起引擎(127.0.0.1:18060/18061)后:

| 步骤 | 结果(实测) |
|---|---|
| 新增订阅 | rule=3 host=2 ip=1 rewrite=4 script=1 skipped=0 |
| rules.sub.json | ssl_bypass(*.blocked) / header(api.../v1/user) / map_local(/echo) / host_block(ads) |
| scope.sub.json | domains=[*.smoke.example.com]([mitm] %. + DOMAIN-SUFFIX 去重合并) |
| 脚本转换 | $response.body→flow.response.body, $done({body})→赋值+return, URL 守卫生成 |
| 暂停订阅 | rules.sub.json 清空、scope.sub.json 域名清空 |
| 重新启用+立即更新 | 三层文件恢复 |
| 删除订阅 | subs 列表空 |
| 引擎重启自愈 | 订阅列表/生效范围/三层文件完整恢复(NewManager 启动时重放) |
| IP/GEOIP 冲突 | 域名订阅中 IP-CIDR 不应用于范围(AND 冲突), 记 warning |

## 三、WebUI 内嵌核查

```bash
cd /opt/runhooks-engine && bash scripts/check_ui.sh
```

- index.html base=/plugin_proxy/runhooks/(3 处引用)
- JS bundle 含订阅面板全部关键词(规则订阅/订阅地址/适配说明/更新于/QuanX/纯列表)
- /api/health 200

## 四、插件 netns 隔离测试(宿主机零规则)

```bash
cd /opt/runhooks-engine && bash scripts/netns_plugin_test.sh
```

模拟 ikcommon 分发(source + local key=value + Show TYPE 分发), 全部在 `unshare -n` 内:

| 用例 | 结果(实测) |
|---|---|
| 引擎启动 | 3/3 端口监听, health ok, pidfile 落盘 |
| 开抓包 | nat RUNHOOKS_REDIRECT: 路由器 IP RETURN + 设备 MAC REDIRECT→18062; filter RUNHOOKS_QUIC: UDP443 DROP |
| 订阅添加(经 service.sh→引擎 API) | rules.sub.json 三条规则正确(host/path), scope.sub.json 域名正确 |
| show TYPE=subs | 回传 subs+effective+engine:true(页面数据通道) |
| 关抓包 | REDIRECT/PREROUTING 残留 0 |
| 删订阅 | rules 归 0 |
| 停引擎 | 端口释放, /tmp/iktmp/plugin_web/runhooks 端口文件清除 |
| 最终核查 | netns 内自定义链 0; **宿主机 nat/filter/端口全部 0(零污染)** |

## 五、打包核查

```bash
cd /opt/ikuai4-plugins && rm -rf /tmp/oneplug && mkdir -p /tmp/oneplug \
  && cp -a plugins/runhooks /tmp/oneplug/ && python3 build.py /tmp/oneplug /tmp/rhout
tar -tzf /tmp/rhout/ipk/plugin-runhooks-*.ipk
```

- 包内 6 项齐全: html/{index.html,logo.png,metadata.json} script/service.sh install.sh bins/runhooks-x86_64
- 二进制 19.66MB, **PT_INTERP 为空(全静态, 设备 uClibc 无 glibc loader)**
- metadata: name=runhooks version=1.0.0 alias=Runhooks抓包
- ipk 7.45MB > 6MB → **只能在线安装**(页面上传必失败, 已知边界)

## 六、设备验收(待用户真机验证, TC-01~TC-18)

| # | 用例 | 命令/操作 |
|---|---|---|
| TC-01 | 在线安装 | 插件管理→在线安装 runhooks(需先 updateSoftwareSouce; repo_id=5f74366d-090d-4540-a04f-d1d885f0b93f) |
| TC-02 | 启动引擎 | 页面点启动→status=1, 18060/18061/18062 监听 |
| TC-03 | 显式代理抓包 | 设备设代理 router:18060 → curl http/https 均出现在引擎界面 |
| TC-04 | 透明抓包 | 勾选设备→开抓包→该设备 HTTPS 站点出现明文 |
| TC-05 | 非选中设备零捕获 | 未勾选设备流量不进 flows |
| TC-06 | CA 下载安装 | 页面下载 CA 证书(base64 通道, HTTPS 无混合内容) |
| TC-07 | 请求改写 | 引擎界面加 header 规则→上游回显验证 |
| TC-08 | 返回改写 | body_replace 规则→响应体被替换 |
| TC-09 | 断点 | 断点挂请求→两阶段(请求+响应)分别放行 |
| TC-10 | 脚本 | 应用内置模板→命中/未命中分别验证 |
| TC-11 | 订阅添加(QuanX) | 订阅 tab 粘订阅地址→生效范围出现域名 |
| TC-12 | 订阅适配规则 | rules.sub.json 内容与页面明细一致 |
| TC-13 | 订阅脚本转换 | QX rewrite 脚本→引擎脚本→端到端改写生效 |
| TC-14 | 订阅暂停/启用 | 三层文件随之清空/恢复 |
| TC-15 | 订阅自动更新 | interval_min=5→到点自动重拉 |
| TC-16 | 订阅重启自愈 | 重启路由器→订阅与生效范围恢复 |
| TC-17 | 改端口 | set_proxy_ports→引擎重启听新端口, iframe 跟随 |
| TC-18 | 停止 | 停引擎→iptables 链撤净、端口文件清除、设备网络恢复 |

## 已知边界(非缺陷)

1. ipk 7.45MB > 6MB, 只能在线安装(页面上传必失败)。
2. 压缩响应(gzip/br)的明文替换不生效——需先加 `Accept-Encoding: identity` 请求头规则(引擎 KeepAcceptEncoding 已开)。
3. 断点一个流会挂两次(请求阶段+响应阶段), 都要放行客户端才收到内容。
4. 显式代理(18060)不受 iptables 规则约束——关了抓包总开关会同时暂停引擎记录层(已处理)。
5. 引擎界面 proxy 地址显示写死默认值, 以本页端口为准。
6. 域名订阅与 IP-CIDR/GEOIP 同时存在时, IP/国家维度不应用(引擎三维度 AND 语义, 应用会把域名流量全部挡掉)——解析明细里可见原始规则。
7. QuanX DOMAIN-SET/IP-ASN/AND/NOT/FINAL/PROCESS-NAME 等类型暂不支持(跳过并计数)。
8. Surge Map Local/Header Rewrite/URL Rewrite 段暂不支持(跳过并计数)。
9. 脚本引用的相对路径按订阅 conf 所在目录解析; 取不到内容的脚本跳过并 warning。
10. WebUI 经固件同源代理 /plugin_proxy/runhooks/ 访问需 Build202611181800+; 旧固件回退 0.0.0.0 直连(功能优先)。
11. HTTPS 管理页嵌 HTTP 引擎 UI 的混合内容问题: 新固件走同源代理已解; 旧固件直连形态下 iframe 可能被浏览器拦(已知边界)。
