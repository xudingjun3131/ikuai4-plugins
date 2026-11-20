#!/bin/bash
# netfence P1 回归自测: 复现测试报告 TEST-REPORT-2026-11-20-netfence 的失败形态。
# 框架是经 /usr/ikuai/function/plugin_<name> 软链调用 service.sh 的, 旧版用
# dirname "$0" 定位 INSTALL_DIR, 落到软链所在目录, BIN 指向不存在的路径,
# 导致 start 失败 / show 版本 unknown / plugin_proxy 无端口文件 502。
#
# 本脚本在临时目录里搭出与真机同构的目录树, 用软链调用 service.sh,
# 断言: ① __bin_check 通过 ② BIN 指向真实二进制 ③ __show_data 版本非 unknown
#       ④ start 写出代理端口文件 ⑤ 引擎 API 可达 ⑥ stop 摘掉端口文件。
# 纯构建机运行, 不碰设备、不需要 root(用临时目录替代 /usr/ikuai 与 /tmp/iktmp)。
set -u
ROOT="$(mktemp -d)"
FAIL=0
ok(){ echo "  PASS  $1"; }
bad(){ echo "  FAIL  $1"; FAIL=1; }
cleanup(){
    [ -f "$ROOT/config/netfence/netfenced.pid" ] && kill "$(cat "$ROOT/config/netfence/netfenced.pid" 2>/dev/null)" 2>/dev/null
    sleep 0.3
    rm -rf "$ROOT"
}
trap cleanup EXIT

SRC="/opt/ikuai4-plugins/plugins/netfence"
# 真机同构: <install_root>/netfence/{bin,script,html}
mkdir -p "$ROOT/store/netfence/bin" "$ROOT/store/netfence/script" "$ROOT/store/netfence/html" \
         "$ROOT/function" "$ROOT/config/netfence/lists" "$ROOT/config/netfence/threat" "$ROOT/log"
cp "$SRC/bins/netfence-x86_64" "$ROOT/store/netfence/bin/netfenced"
chmod +x "$ROOT/store/netfence/bin/netfenced"
cp "$SRC/script/service.sh" "$ROOT/store/netfence/script/service.sh"
cp "$SRC/script/default-policy.json" "$ROOT/store/netfence/script/default-policy.json"
# 框架软链: 绝对路径, 与 install.sh 的 ln -sf 一致
ln -s "$ROOT/store/netfence/script/service.sh" "$ROOT/function/plugin_netfence"

# service.sh 里 PP_PORT_FILE 固定 /tmp/iktmp/plugin_web/netfence; 用 sed 把测试副本
# 指到临时目录(不动源码), 其余逻辑逐字保留。
sed -i "s#^PP_PORT_FILE=.*#PP_PORT_FILE=$ROOT/port_web/netfence#" \
    "$ROOT/store/netfence/script/service.sh"

export EXT_PLUGIN_CONFIG_DIR="$ROOT/config"
export EXT_PLUGIN_LOG_DIR="$ROOT/log"
export EXT_PLUGIN_INSTALL_DIR="$ROOT/store"
# 与真机一致: 经软链调用, $0 是软链路径
SVC="$ROOT/function/plugin_netfence"

echo "=== netfence P1 回归: 软链调用形态 ==="
echo "  软链: $SVC -> $(readlink "$SVC")"

# ① ② ③ : source 脚本后检查自定位结果(show 不经网络也能读二进制版本)
OUT=$(bash -c '
  . "$1" >/dev/null 2>&1
  printf "BIN=%s\n" "$BIN"
  if __bin_check >/dev/null 2>&1; then echo "BINCHECK=1"; else echo "BINCHECK=0"; fi
  __show_data >/dev/null 2>&1
  printf "SHOW=%s\n" "$__json_result__"
' _ "$SVC" 2>&1)
echo "$OUT" | grep -E '^(BIN|BINCHECK|SHOW)=' | sed 's/^/    /'

echo "$OUT" | grep -q 'BINCHECK=1' && ok "① __bin_check 通过(软链下 BIN 可定位)" || bad "① __bin_check 仍失败"
REALBIN="$ROOT/store/netfence/bin/netfenced"
echo "$OUT" | grep -q "BIN=$REALBIN" && ok "② BIN 指向真实二进制" || bad "② BIN 路径错误"
echo "$OUT" | grep -q '"version":"1.0.1"' && ok "③ show 读到引擎版本(非 unknown)" || bad "③ 版本仍 unknown"
echo "$OUT" | grep -q 'plugin web ui' && bad "③ show 混入异常文本" || true

# ④ ⑤ : 真起引擎(start 经软链调用), 验端口文件 + API
bash -c '. "$1" >/dev/null 2>&1; start' _ "$SVC" >"$ROOT/start.out" 2>&1
START_RC=$?
echo "  start rc=$START_RC out=$(tr -d '\n' < "$ROOT/start.out" | head -c 120)"
PORT_FILE="$ROOT/port_web/netfence"
[ "$START_RC" -eq 0 ] && ok "④ start 返回 0" || bad "④ start 返回非 0"
[ -f "$PORT_FILE" ] && [ "$(cat "$PORT_FILE")" = "18111" ] \
    && ok "⑤ 代理端口文件已写($PORT_FILE=$(cat "$PORT_FILE"))" \
    || bad "⑤ 代理端口文件缺失(新固件会 502)"
HEALTH=$(curl -s --max-time 3 http://127.0.0.1:18111/api/health 2>/dev/null)
echo "$HEALTH" | grep -q '"ok":true' && ok "⑥ 引擎 API 可达: $HEALTH" || bad "⑥ 引擎 API 不可达: ${HEALTH:-空}"

# ⑦ stop 摘端口文件
bash -c '. "$1" >/dev/null 2>&1; stop' _ "$SVC" >/dev/null 2>&1
[ ! -f "$PORT_FILE" ] && ok "⑦ stop 摘掉端口文件" || bad "⑦ stop 未清理端口文件"

echo "=== 结果: $([ $FAIL -eq 0 ] && echo ALL_PASS || echo HAS_FAILURE) ==="
exit $FAIL
