#!/usr/bin/env bash
# runhooks v1.1.7 —— 构建机冒烟: sub_add / sub_meta 在「设备固件工具形态」下的回归
#
# 背景一(v1.1.5 复测报告 §4.2/§4.3): 设备固件自带 jq 与构建机 GNU jq 形态不一致,
# jq -nc --argjson ... 在设备上恒返回空, 导致页面点「添加订阅」恒回
# 「参数构造失败(订阅地址含非法字符)」, 而同一 URL 直连引擎 201 成功。
# v1.1.5 修好了 __engine_api 统一通道, 却没同步修 body 构造层; 构建机是完整
# GNU jq, 冒烟 TC-02/TC-03 全 PASS 也照样在真机挂 —— 这是本脚本要防的假绿。
# 背景二(v1.1.6 复测报告 §四, P3): v1.1.6 真机 P1 已销账, 但重复 URL 二次点击
# 回「引擎 API 未返回内容 + jq stderr」——设备 GNU wget 对 4xx 静默丢弃响应体,
# 引擎业务原文「该订阅地址已存在」没透传到页面。v1.1.7 让 __engine_api 透传 4xx
# 响应体(curl -w 状态码 / wget --content-on-error), sub_add 回引擎业务原文,
# jqerr 只在 body 构造失败分支回显。本脚本新增 TC-03 页面文案判据与 wget 形态用例。
#
# 本脚本用 shim 模拟设备固件 jq / 老 wget, 直接调用(经桩环境 source 的)真实
# service.sh 里的 sub_add / sub_meta, 让请求走真实本地引擎。jq 四种形态:
#   ① GNU jq(构建机原形态)       —— 主路径 --arg + tonumber
#   ② 设备 jq(仅拒 --argjson)    —— 忠实复刻复测报告观测形态
#   ③ 设备 jq(拒整条 -n/--arg)   —— 更严苛, 逼出无 jq 纯 shell 兜底
#   ④ jq 完全缺位                —— 兜底 + 成功判定退化
# wget 两种形态: ⑤ GNU wget(支持 --content-on-error) ⑥ 老 wget(拒该选项, 走反查兜底)。
# 外加: 旧 --argjson 写法在 ①/② 下的差异对照(互锁证据)、TC-02/TC-03 真引擎链路、
# sub_meta 同层、body 合法性(含 " \ TAB 换行 的 JSON 转义)校验、现场清理。
#
# 边界(如实声明): 这是【构建机冒烟】, 不是真机点按钮。TC-02/TC-03 的销账仍须
# 按 TEST-REPORT-2026-11-20-v1.1.6-retest.md 第五节复验入口,
# 在桌面可见 headful Chrome 真机点一次按钮才算。
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

# ---- 老设备 wget-ssl 形态模拟器 ----
# 真机 wget-ssl 是 GNU wget(支持 --method/--body-data), 但为防设备上是更老版本,
# 另造一个「拒绝 --content-on-error」的形态, 逼出 __engine_api 的重试 +
# sub_add 的订阅列表反查兜底, 验证 4xx 文案在任何工具形态下都不会退化。
cat > "$SHIMDIR/wget-ssl" <<'WSHIM'
#!/usr/bin/env bash
for a in "$@"; do
	case "$a" in
		--content-on-error) echo "wget: unrecognized option '--content-on-error'" >&2; exit 2 ;;
	esac
done
exec /usr/bin/wget "$@"
WSHIM
chmod 755 "$SHIMDIR/wget-ssl"
REAL_WGET=/usr/bin/wget

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
# TC-03 页面文案判据: 重复同 URL 必须回引擎业务原文「该订阅地址已存在」,
# 且不得再出现「参数构造失败 / 未返回内容 / jq stderr」等误导性归因。
check_dup_text()
{
	local label=$1
	case "$ADD_OUT" in
		*该订阅地址已存在*) ;;
		*) ng "$label: 未回业务文案: $ADD_OUT"; return 0 ;;
	esac
	if printf '%s' "$ADD_OUT" | grep -qE '参数构造失败|未返回内容|工具 stderr|jq:|引擎 API'; then
		ng "$label: 业务文案被无关归因污染: $ADD_OUT"
		return 0
	fi
	ok "$label: 页面回引擎业务原文 [$ADD_OUT]"
}

echo "==================================================================="
echo "构建机冒烟 v1.1.7 / 设备固件 jq + wget 形态用例  engine=$ENGINE_BIN"
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
# TC-03: 同一 URL 再点 -> 页面回引擎业务原文「该订阅地址已存在」(v1.1.7 新增文案判据)
add_once "GNU jq 二次 add(同 URL)" "$U1"
check_dup_text "TC-03/curl 通道"
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

# ---------- 5) 设备 wget-ssl(GNU wget, 支持 --content-on-error) -> 4xx 原文透传 ----------
echo "-- [5] 设备 wget-ssl 形态: --content-on-error 透传 4xx 响应体"
__WGET_SSL="$REAL_WGET"
U5="http://127.0.0.1:$PICK_SRV/sub.conf?case=5&b=2"
add_once "wget-ssl 首次 add" "$U5"
check_add "wget-ssl" "$U5"
add_once "wget-ssl 二次 add(同 URL)" "$U5"
check_dup_text "TC-03/wget-ssl 通道"
__WGET_SSL=""

# ---------- 6) 老设备 wget-ssl(拒 --content-on-error) -> 重试 + 订阅反查兜底 ----------
echo "-- [6] 老设备 wget-ssl 形态: 拒 --content-on-error, 订阅列表反查兜底"
__WGET_SSL="$SHIMDIR/wget-ssl"
U6="http://127.0.0.1:$PICK_SRV/sub.conf?case=6&b=2"
add_once "old-wget 首次 add" "$U6"
check_add "old-wget" "$U6"
add_once "old-wget 二次 add(同 URL)" "$U6"
check_dup_text "TC-03/old-wget 兜底"
__WGET_SSL=""

# ---------- 7) 现场清理(清空自建订阅) ----------
echo "-- [7] 现场清理(清空自建订阅)"
curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print("\n".join(s["id"] for s in json.load(sys.stdin)["subs"]))' | while read -r sid; do
	[ -n "$sid" ] && curl -s -X DELETE "http://127.0.0.1:$PICK_API/api/subscriptions/$sid" >/dev/null
done
FINAL=$(curl -s "http://127.0.0.1:$PICK_API/api/subscriptions" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["subs"]))')
[ "$FINAL" -eq 0 ] && ok "现场恢复 subs=0" || ng "现场未清空 subs=$FINAL"

echo "==================================================================="
echo "汇总: PASS=$PASS FAIL=$FAIL"
echo "声明: 以上全部为【构建机冒烟】。TC-02/TC-03 真机点按钮销账仍待按"
echo "       TEST-REPORT-2026-11-20-v1.1.6-retest.md 第五节复验入口。"
echo "==================================================================="
[ "$FAIL" -eq 0 ] || exit 1
