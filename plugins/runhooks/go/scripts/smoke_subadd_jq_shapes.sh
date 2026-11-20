#!/usr/bin/env bash
# runhooks v1.1.6 —— 构建机冒烟: sub_add / sub_meta body 构造在「设备固件 jq 形态」下的回归
#
# 背景(v1.1.5 复测报告 §4.2/§4.3): 设备固件自带 jq 与构建机 GNU jq 形态不一致,
# jq -nc --argjson ... 在设备上恒返回空, 导致页面点「添加订阅」恒回
# 「参数构造失败(订阅地址含非法字符)」, 而同一 URL 直连引擎 201 成功。
# v1.1.5 修好了 __engine_api 统一通道, 却没同步修 body 构造层; 构建机是完整
# GNU jq, 冒烟 TC-02/TC-03 全 PASS 也照样在真机挂 —— 这是本脚本要防的假绿。
#
# 本脚本用 shim 模拟设备固件 jq, 直接调用(经桩环境 source 的)真实 service.sh
# 里的 sub_add / sub_meta, 让请求走真实本地引擎。三种 jq 形态各跑一遍:
#   ① GNU jq(构建机原形态)       —— 主路径 --arg + tonumber
#   ② 设备 jq(仅拒 --argjson)    —— 忠实复刻复测报告观测形态
#   ③ 设备 jq(拒整条 -n/--arg)   —— 更严苛, 逼出无 jq 纯 shell 兜底
#   ④ jq 完全缺位                —— 兜底 + 成功判定退化
# 外加: 旧 --argjson 写法在 ①/② 下的差异对照(互锁证据)、TC-02/TC-03 真引擎链路、
# sub_meta 同层、body 合法性(含 " \ TAB 换行 的 JSON 转义)校验、现场清理。
#
# 边界(如实声明): 这是【构建机冒烟】, 不是真机点按钮。TC-02/TC-03 的销账仍须
# 按复测报告第六节入口, 在桌面可见 headful Chrome 真机点一次按钮才算。
#
# 用法: bash smoke_subadd_jq_shapes.sh   (可设 BIN= 指定引擎二进制)
set -o pipefail
export PATH=/usr/local/go/bin:$PATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
GO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
PLUGIN_DIR=$(cd "$GO_DIR/.." && pwd)
SERVICE="$PLUGIN_DIR/script/service.sh"
BIN=${BIN:-$PLUGIN_DIR/bins/runhooks-x86_64}
ENGINE_BIN=$BIN          # source service.sh 会覆写 BIN 变量, 先存一份供日志显示

WORK=$(mktemp -d /opt/ikuai4-plugins/.smoke-data/rhjq.XXXXXX 2>/dev/null || mktemp -d /tmp/rhjq.XXXXXX)
SHIMDIR="$WORK/bin"
CAPTURE="$WORK/captured_body.json"
JQ_SHIM_LOG="$WORK/jq_shim.log"
export JQ_SHIM_LOG
mkdir -p "$SHIMDIR" "$WORK/install" "$WORK/config" "$WORK/log" "$WORK/data" "$WORK/src"

ENG=""; SRVPID=""
cleanup()
{
	[ -n "$ENG" ] && kill "$ENG" 2>/dev/null
	[ -n "$SRVPID" ] && kill "$SRVPID" 2>/dev/null
	wait 2>/dev/null
	rm -rf "$WORK"
}
trap cleanup EXIT

PASS=0; FAIL=0
ok(){ echo "  [PASS] $1"; PASS=$((PASS+1)); }
ng(){ echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }

pick_port(){ python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }
PICK_API=$(pick_port); PICK_PROXY=$(pick_port); PICK_SRV=$(pick_port)

# ---- 设备固件 jq 形态模拟器 ----
# 管道过滤(jq -c / -r / -e)正常工作 -> 委派构建机 GNU jq;
# 对象构造(-n / --arg* / --argjson)返回空 + stderr -> 复现设备。
cat > "$SHIMDIR/jq" <<'SHIM'
#!/usr/bin/env bash
LOG="${JQ_SHIM_LOG:?JQ_SHIM_LOG unset}"
MODE="${JQ_SHIM_MODE:-strict}"
for a in "$@"; do printf '%s\n' "$a" >> "$LOG"; done
for a in "$@"; do
	case "$a" in
		--argjson) echo "jq: (firmware build) unknown option --argjson" >&2; exit 2 ;;
	esac
done
if [ "$MODE" = strict ]; then
	for a in "$@"; do
		case "$a" in
			-n|-nc|-nr|-na|--arg|--args|--kwargs|--argfile|--slurpfile|--rawfile)
				echo "jq: (firmware build) unsupported object construction: $a" >&2; exit 2 ;;
		esac
	done
fi
exec /usr/bin/jq "$@"
SHIM
chmod 755 "$SHIMDIR/jq"

# ---- 本地订阅源 + 真引擎(仅回环) ----
printf '[mitm]\nhostname = qa.netloom.test, *.smoke.example.com\nDOMAIN-SUFFIX,smoke.example.com,PROXY\n' > "$WORK/src/sub.conf"
python3 -m http.server "$PICK_SRV" --bind 127.0.0.1 --directory "$WORK/src" >/dev/null 2>&1 &
SRVPID=$!
for i in $(seq 1 20); do curl -s -o /dev/null "http://127.0.0.1:$PICK_SRV/sub.conf" && break; sleep 0.2; done

"$BIN" -proxy-addr 127.0.0.1:$PICK_PROXY -api-addr 127.0.0.1:$PICK_API -data-dir "$WORK/data" > "$WORK/engine.log" 2>&1 &
ENG=$!
for i in $(seq 1 40); do curl -s -o /dev/null "http://127.0.0.1:$PICK_API/api/health" && break; sleep 0.3; done
if ! curl -s "http://127.0.0.1:$PICK_API/api/health" | grep -q '"ok"'; then
	echo "引擎未就绪, 日志:"; cat "$WORK/engine.log"; exit 1
fi

# ---- 桩环境 source 真实 service.sh(无 /etc/ikcommon) ----
export EXT_PLUGIN_INSTALL_DIR="$WORK/install"
export EXT_PLUGIN_CONFIG_DIR="$WORK/config"
export EXT_PLUGIN_LOG_DIR="$WORK/log"
export EXT_PLUGIN_IPK_DIR="$WORK/ipk"
source "$SERVICE"
API_PORT=$PICK_API          # service.sh 顶层写死 18061, source 后覆盖
__WGET_SSL=""               # 强制走 curl 回退分支(构建机冒烟口径)
ORIG_PATH="$PATH"
JQ_REAL=$(declare -f __engine_api)
eval "${JQ_REAL//__engine_api/__engine_api_real}"
# 拦截 body 供逐用例校验, 再原样转发给真引擎
__engine_api()
{
	if [ -n "${3:-}" ]; then printf '%s' "$3" > "$CAPTURE"; fi
	__engine_api_real "$@"
}

NAME_PLAIN='v116冒烟"带引号\反斜杠'
EXPECT_NAME="$NAME_PLAIN"; export EXPECT_NAME
add_once()
{
	local label=$1 url=$2
	rm -f "$CAPTURE"
	url_b64=$(printf '%s' "$url" | base64 -w0)
	name_b64=$(printf '%s' "$NAME_PLAIN" | base64 -w0)
	format=auto
	interval_min=0
	ADD_OUT=$(sub_add 2>&1); ADD_RC=$?
	ADD_BODY=$(cat "$CAPTURE" 2>/dev/null)
	echo "  $label => rc=$ADD_RC out=[$ADD_OUT]"
}
check_add()
{
	local label=$1 url=$2
	[ "$ADD_RC" -eq 0 ] && [ "$ADD_OUT" = "订阅已添加并生效" ] \
		&& ok "$label: sub_add 成功" || ng "$label: sub_add 未成功(rc=$ADD_RC out=$ADD_OUT)"
	printf '%s' "$ADD_BODY" > "$WORK/body.json"
	if EXPECT_URL="$url" python3 - "$WORK/body.json" <<'PY'
import json, sys, os
d = json.load(open(sys.argv[1]))
assert d.get("url") == os.environ["EXPECT_URL"], d
assert d.get("format") == "auto", d
assert d.get("interval_min") == 0, d
assert d.get("name") == os.environ["EXPECT_NAME"], d
print("BODY_OK " + json.dumps(d, ensure_ascii=False))
PY
	then ok "$label: body 合法 JSON 且 URL/特殊字符/字段全对"
	else ng "$label: body 非法或字段错"; fi
}

echo "==================================================================="
echo "构建机冒烟 v1.1.6 / 设备固件 jq 形态用例  engine=$ENGINE_BIN"
echo "health=$(curl -s http://127.0.0.1:$PICK_API/api/health)"
echo "==================================================================="

# ---------- 0) 互锁对照: 旧 --argjson 写法在两种 jq 形态下的差异 ----------
echo "-- [0] 互锁对照: 复现『构建机冒烟 PASS 却真机 FAIL』"
OLD='{url:$u,name:$n,format:$f,interval_min:$i}'
U0="http://127.0.0.1:$PICK_SRV/sub.conf?probe=1"
full_old=$(jq -nc --arg u "$U0" --arg n n --arg f auto --argjson i 0 "$OLD" 2>&1); full_rc=$?
: > "$JQ_SHIM_LOG"
dev_old=$(JQ_SHIM_MODE=faithful PATH="$SHIMDIR:$ORIG_PATH" jq -nc --arg u "$U0" --arg n n --arg f auto --argjson i 0 "$OLD" 2>"$WORK/dev_err"); dev_rc=$?
dev_err=$(tr '\n' ' ' < "$WORK/dev_err")
echo "  GNU jq    : rc=$full_rc out=[$full_old]"
echo "  设备jq shim: rc=$dev_rc body=[$dev_old] stderr=[$dev_err]"
# 判据: GNU jq 出合法 body; 设备形态 stdout 无 body, stderr 直指 --argjson 真因
if [ "$full_rc" -eq 0 ] && echo "$full_old" | grep -q 'interval_min' && \
   [ "$dev_rc" -ne 0 ] && [ -z "$dev_old" ] && echo "$dev_err" | grep -q -- '--argjson'; then
	ok "对照成立: --argjson 在 GNU jq 出 body、在设备形态返回空且 stderr 直指 --argjson —— 正是历史假绿根因, 也是本版回显 jq stderr 的依据"
else
	ng "对照未复现, 检查 shim"
fi

# ---------- 1) GNU jq(构建机原形态) + TC-02/TC-03 真引擎链路 ----------
echo "-- [1] GNU jq 主路径 + TC-02/TC-03"
: > "$JQ_SHIM_LOG"
U1="http://127.0.0.1:$PICK_SRV/sub.conf?case=1&b=2"
add_once "GNU jq 首次 add" "$U1"
check_add "GNU jq" "$U1"
S1=$(curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["subs"]))')
[ "$S1" -ge 1 ] && ok "TC-02: 真引擎 /api/subscriptions 出现新条目(subs=$S1)" || ng "TC-02: 引擎未出现新条目(subs=$S1)"
# TC-03: 同一 URL 再点 -> 请求确实到达引擎(回『已存在』), 不再是『参数构造失败』
add_once "GNU jq 二次 add(同 URL)" "$U1"
case "$ADD_OUT" in
	*参数构造失败*|*未返回内容*) ng "TC-03: 仍卡在 body 构造层: $ADD_OUT" ;;
	*) ok "TC-03: 请求到达引擎, 回 [$ADD_OUT](非构造失败)" ;;
esac
S2=$(curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["subs"]))')
[ "$S2" -eq "$S1" ] && ok "TC-03: 重复 add 未新增条目(subs 仍 $S2)" || ng "TC-03: subs 异常变化($S1->$S2)"

# ---------- 2) 设备固件 jq 形态(忠实: 仅拒 --argjson) ----------
echo "-- [2] 设备固件 jq 形态 · faithful(仅拒 --argjson, 复测报告观测形态)"
: > "$JQ_SHIM_LOG"
export PATH="$SHIMDIR:$ORIG_PATH"; export JQ_SHIM_MODE=faithful
U2="http://127.0.0.1:$PICK_SRV/sub.conf?case=2&b=2"
add_once "faithful add" "$U2"
check_add "faithful" "$U2"
if grep -q -- '--argjson' "$JQ_SHIM_LOG"; then ng "faithful: 代码仍调用了 --argjson"; else ok "faithful: 全程无 --argjson 调用"; fi
if grep -q -- '--arg' "$JQ_SHIM_LOG"; then ok "faithful: 主路径 --arg + tonumber 在设备形态下成立"; else ng "faithful: 未见 --arg 调用"; fi

# ---------- 3) 设备固件 jq 形态(严格: 拒整条 -n/--arg) -> 逼出兜底 ----------
echo "-- [3] 设备固件 jq 形态 · strict(连 -n/--arg 都不支持) -> 纯 shell 兜底"
: > "$JQ_SHIM_LOG"
export JQ_SHIM_MODE=strict
U3="http://127.0.0.1:$PICK_SRV/sub.conf?case=3&b=2"
add_once "strict add" "$U3"
check_add "strict" "$U3"
if grep -q -- '-nc' "$JQ_SHIM_LOG" && ! grep -q -- '--argjson' "$JQ_SHIM_LOG"; then
	ok "strict: jq 构造被调用且被设备形态拒绝, 已走无 jq 兜底且无 --argjson"
else
	ng "strict: 兜底路径未按预期触发"
fi
# 同形态下 sub_meta(复测 §4.2 实测同层失败)一并验
SUBID=$(curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print(json.load(sys.stdin)["subs"][-1]["id"])')
rm -f "$CAPTURE"
id="$SUBID"; name_b64=$(printf '%s' 'submeta改名' | base64 -w0); interval_min=5
META_OUT=$(sub_meta 2>&1); META_RC=$?
echo "  sub_meta => rc=$META_RC out=[$META_OUT]"
[ "$META_RC" -eq 0 ] && [ "$META_OUT" = "已保存" ] && ok "strict: sub_meta 同层修复成功" || ng "strict: sub_meta 失败($META_OUT)"
printf '%s' "$(cat "$CAPTURE" 2>/dev/null)" > "$WORK/meta.json"
if python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));assert d["name"]=="submeta改名" and d["interval_min"]==5,d' "$WORK/meta.json" 2>/dev/null; then
	ok "strict: sub_meta body 合法且字段正确"
else
	ng "strict: sub_meta body 非法"
fi
export PATH="$ORIG_PATH"; unset JQ_SHIM_MODE

# ---------- 4) jq 完全缺位(模拟) -> 兜底 + 成功判定退化 ----------
echo "-- [4] jq 完全缺位(模拟) -> 纯 shell 兜底"
jq(){ return 127; }
U4="http://127.0.0.1:$PICK_SRV/sub.conf?case=4&b=2"
add_once "no-jq add" "$U4"
check_add "no-jq" "$U4"
unset -f jq

# ---------- 5) 现场清理 ----------
echo "-- [5] 现场清理(清空自建订阅)"
curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print("\n".join(s["id"] for s in json.load(sys.stdin)["subs"]))' | while read -r sid; do
	[ -n "$sid" ] && curl -s -X DELETE "http://127.0.0.1:$PICK_API/api/subscriptions/$sid" >/dev/null
done
FINAL=$(curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["subs"]))')
[ "$FINAL" -eq 0 ] && ok "现场恢复 subs=0" || ng "现场未清空 subs=$FINAL"

echo "==================================================================="
echo "汇总: PASS=$PASS FAIL=$FAIL"
echo "声明: 以上全部为【构建机冒烟】。TC-02/TC-03 真机点按钮销账仍待按"
echo "       TEST-REPORT-2026-11-20-v1.1.5-retest.md 第六节入口复验。"
echo "==================================================================="
[ "$FAIL" -eq 0 ] || exit 1
