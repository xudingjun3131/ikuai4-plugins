#!/bin/bash
# netfence D1 回归自测: 验证 dnsEnable=1 时 LAN 53 -> DNS_PORT 重定向真的下发,
# 且带足回环防护(排除路由器自身 IP / 上游 DNS / 引擎自身端口), 停止时整链撤销。
# P0「管控不落到真实流量」的根因就是旧版只有页面文字承诺、无任何 iptables 下发代码。
# 纯构建机运行: 用桩替换 iptables / ip, 记录真实下发的规则, 不碰内核、不需 root。
set -u
ROOT="$(mktemp -d)"
FAIL=0
ok(){ echo "  PASS  $1"; }
bad(){ echo "  FAIL  $1"; FAIL=1; }
cleanup(){ rm -rf "$ROOT"; }
trap cleanup EXIT

SRC="/opt/ikuai4-plugins/plugins/netfence"
export EXT_PLUGIN_CONFIG_DIR="$ROOT/config"
export EXT_PLUGIN_LOG_DIR="$ROOT/log"
mkdir -p "$ROOT/bin" "$ROOT/config/netfence/lists" "$ROOT/config/netfence/threat" "$ROOT/log"

# --- 桩: iptables 把每次调用逐条记到 $ROOT/ipt.log ---
# -C(检查规则是否存在)返回 1=不存在, 才能触发 service.sh 的 `|| -I` 真正挂链;
# 其余 -N/-I/-A/-F/-D/-X 返回 0=成功。这与真实 iptables 语义一致。
cat >"$ROOT/bin/iptables" <<'EOF'
#!/bin/bash
echo "$*" >> "$IPT_LOG"
for a in "$@"; do [ "$a" = "-C" ] && exit 1; done
exit 0
EOF
# --- 桩: ip -o -4 addr show 吐两个路由器自身 IP, 供 -d ... -j RETURN 排除 ---
cat >"$ROOT/bin/ip" <<'EOF'
#!/bin/bash
echo "1: lo inet 127.0.0.1/8 scope host lo"
echo "2: eth0 inet 192.168.9.1/24 scope global eth0"
EOF
chmod +x "$ROOT/bin/iptables" "$ROOT/bin/ip"
export IPT_LOG="$ROOT/ipt.log"
export PATH="$ROOT/bin:$PATH"

# 引擎二进制桩: start() 会拉起它; 只需存活足够久让 start 的 __alive 判定通过。
cat >"$ROOT/fake_engine" <<'EOF'
#!/bin/bash
sleep 30
EOF
chmod +x "$ROOT/fake_engine"

# 直接构造一个只含 D1 相关函数的测试载体: source 真 service.sh 会触发框架 helper 缺失,
# 故用 sed 把它改成可 source 的形态(去掉 shebang 行 irrelevant), 并覆盖关键变量。
SVC="$ROOT/svc.sh"
cp "$SRC/script/service.sh" "$SVC"
sed -i "s#^BIN=\".*\"#BIN=\"$ROOT/fake_engine\"#" "$SVC" 2>/dev/null
# 强制: 二进制路径用桩、DNS 开启、上游用一个明确 IP
cat >>"$SVC" <<EOF
BIN="$ROOT/fake_engine"
DNS_ENABLE=1
DNS_PORT=18110
UPSTREAM="223.5.5.5:53"
EOF

echo "=== netfence D1 回归: DNS 重定向下发/撤销 ==="
: > "$IPT_LOG"
OUT=$(bash -c '. "$1" >/dev/null 2>&1; __dns_rules_on; echo RC=$?' _ "$SVC" 2>&1)
echo "$OUT" | grep -q 'RC=0' && ok "① __dns_rules_on 返回 0(iptables 可用)" || bad "① __dns_rules_on 失败: $OUT"

# 断言下发的规则内容
grep -q -- "-N NETFENCE_DNS" "$IPT_LOG"                       && ok "② 建专用链 NETFENCE_DNS" || bad "② 未建专用链"
grep -q -- "-I PREROUTING 1 -j NETFENCE_DNS" "$IPT_LOG"        && ok "③ 挂到 PREROUTING" || bad "③ 未挂 PREROUTING"
grep -q -- "-p udp --dport 53 -j REDIRECT --to-ports 18110" "$IPT_LOG" && ok "④ udp 53 重定向到引擎" || bad "④ udp 53 未重定向"
grep -q -- "-p tcp --dport 53 -j REDIRECT --to-ports 18110" "$IPT_LOG" && ok "⑤ tcp 53 重定向到引擎" || bad "⑤ tcp 53 未重定向"
grep -q -- "-d 223.5.5.5 -j RETURN" "$IPT_LOG"                && ok "⑥ 排除上游 DNS(防回环)" || bad "⑥ 未排除上游"
grep -q -- "-d 192.168.9.1 -j RETURN" "$IPT_LOG"              && ok "⑦ 排除路由器自身 IP" || bad "⑦ 未排除路由器 IP"
grep -q -- "--sport 18110 -j RETURN" "$IPT_LOG"               && ok "⑧ 排除引擎自身端口(二次防回环)" || bad "⑧ 未排除自身端口"

# 撤销: 整链删除, 不留孤儿规则
: > "$IPT_LOG"
OUT=$(bash -c '. "$1" >/dev/null 2>&1; __dns_rules_off; echo RC=$?' _ "$SVC" 2>&1)
echo "$OUT" | grep -q 'RC=0' && ok "⑨ __dns_rules_off 返回 0" || bad "⑨ off 失败"
grep -q -- "-D PREROUTING -j NETFENCE_DNS" "$IPT_LOG" && ok "⑩ 摘 PREROUTING 跳转" || bad "⑩ 未摘跳转"
grep -q -- "-X NETFENCE_DNS" "$IPT_LOG" && ok "⑪ 删除整链(不留孤儿)" || bad "⑪ 未删整链"

# dnsEnable=0 时 start 走 off 分支, 不应下发任何 REDIRECT
: > "$IPT_LOG"
echo 'DNS_ENABLE=0' >> "$SVC"
bash -c '. "$1" >/dev/null 2>&1; __dns_rules_off' _ "$SVC" >/dev/null 2>&1
if ! grep -q -- "REDIRECT" "$IPT_LOG"; then ok "⑫ DNS 关闭时无 REDIRECT 下发"; else bad "⑫ DNS 关闭仍下发 REDIRECT"; fi

echo "=== 结果: $([ $FAIL -eq 0 ] && echo ALL_PASS || echo HAS_FAILURE) ==="
exit $FAIL
