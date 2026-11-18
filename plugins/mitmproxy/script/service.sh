#!/bin/bash /etc/ikcommon
# Mitmproxy 抓包插件 service.sh
# 架构: 层1 显式代理(客户端手动设代理 router_ip:18070, regular 模式)
#       层2 iptables(本脚本): 按设备(MAC/IP)精确 REDIRECT 到透明端口(transparent 模式),
#           绝不全局; UDP443(QUIC) 对选中设备 DROP, 强制回退 TCP 才能 MITM
#       层3 mitmproxy 自身 MITM 解密 + addons
#       层4 Go 引擎 WebUI(18071)查看/改写/断点/规则/脚本
# 运行时: [go-rewrite] v2.0.0 起换成自研 Go 引擎(bins/mitmproxy-go, CGO_ENABLED=0
#         全静态 ~19MB), 替掉原 Alpine Python3.14+mitmproxy(~100MB): 冷启动毫秒级
#         (原 Python 版要 60s 健康等待), 无 musl loader/Python 依赖, 自带中文 WebUI
#         (不再需要 lang.json 同步/选项汉化注入), API 兼容 mitmweb 语义(/api/flows
#         /api/capture /api/ca.pem /api/events SSE)。
# [transparent] v1.0.2 起两个监听并存: regular@显式端口 + transparent@透明端口
#         (mitmweb --mode 可多次传递, @host:port 逐模式指定), 透明监听常开,
#         流量由 iptables 规则按设备精确导入; 无规则时透明端口空转无害。
PLUGIN_NAME=mitmproxy
. /etc/release
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

# [self-locate] 用脚本自身位置反推插件安装目录, 不盲信 config.sh 的路径。
# 实踩(reqable): 用户换存储卷后 /usr/ikuai/function 软链接与 config.sh 仍指旧位置,
# 启动报 "No such file or directory", 而文件在新位置完好 —— 先解析链接再取目录。
# 目录关系: <INSTALL_DIR>/mitmproxy/script/service.sh
__slf="$0"
if [ -L "$__slf" ]; then __slf=$(readlink "$__slf" 2>/dev/null); fi
[ -n "$__slf" ] || __slf="$0"
__slfdir=$(cd "$(dirname "$__slf")" 2>/dev/null && pwd)
__pdir=$(cd "$__slfdir/.." 2>/dev/null && pwd)        # 插件自己的目录(含 bins/)
__idir=$(cd "$__slfdir/../.." 2>/dev/null && pwd)     # EXT_PLUGIN_INSTALL_DIR
if [ -n "$__pdir" ] && [ -f "$__pdir/bins/mitmproxy-go" ]; then
	[ -n "$__idir" ] && [ "$__idir" != "$EXT_PLUGIN_INSTALL_DIR" ] && EXT_PLUGIN_INSTALL_DIR="$__idir"
	[ -f "$__pdir/../plugins" ] || EXT_PLUGIN_IPK_DIR="$__pdir/../plugins"
fi
unset __slf __slfdir __pdir __idir

# [go-rewrite] Go 静态引擎(替代原 Python 运行时): 显式+透明双监听 + 内嵌中文 WebUI
BIN=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/mitmproxy-go
# [storage-anchor] CA/配置/pid 全落配置目录(存储卷; 未设存储时 p5, 绝不落 sda3)
CONFDIR=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
PIDFILE=$CONFDIR/mitmweb.pid
LOGDIR=$EXT_PLUGIN_LOG_DIR
LOGFILE=$LOGDIR/mitmproxy.log
DEF_PROXY=18070
DEF_WEB=18071
DEF_TPROXY=18072     # 透明监听(iptables REDIRECT 的目标)
PORTS_FILE=$CONFDIR/ports
CAPTURE_FLAG=$CONFDIR/capture_on    # 抓包总开关(iptables 规则)
RULES_FLAG=$CONFDIR/rules_on        # iptables 规则当前是否生效
DEVICES_FILE=$CONFDIR/devices.json  # 选中的设备 [{ip,mac,name}]
DPORTS_FILE=$CONFDIR/dports         # 透明拦截的端口(默认 80,443)
LANG_FILE=$CONFDIR/lang             # 界面语言: zh(默认) | en
# [plugin-proxy] iKuai 加固(Build202611181800 起固件支持): mitmweb 原来直接绑
# 0.0.0.0:18071, 局域网内无痕窗口打开 IP:18071 就能看全部解密流量/改选项——零鉴权,
# 重大隐患(与 reqable 同款)。修法: 固件 OpenResty 加同源代理 /plugin_proxy/mitmproxy/
# (校验 sess_key 后转发到 127.0.0.1), mitmweb 只绑 127.0.0.1, 页面 iframe 走同源相对
# 路径(顺带修复 HTTPS 管理页 mixed-content 拦 iframe)。旧固件回退 0.0.0.0 直连。
__PROXY_MIN_BUILD=202611181800
__web_bind_addr()
{
	local __bd
	__bd=$(grep '^BUILD_DATE=' /etc/release 2>/dev/null | cut -d= -f2)
	case "$__bd" in ''|*[!0-9]*) echo 0.0.0.0; return ;; esac
	if [ "$__bd" -ge "$__PROXY_MIN_BUILD" ]; then echo 127.0.0.1; else echo 0.0.0.0; fi
}
# OpenResty 代理按插件名查此文件找本地端口(启动时写, 停止时删)
PP_PORT_FILE=/tmp/iktmp/plugin_web/mitmproxy

# 读端口配置(文件格式: "代理端口 Web端口 透明端口"); 没配过用默认值
__ports()
{
	local p w t
	p=$(cut -d' ' -f1 $PORTS_FILE 2>/dev/null)
	w=$(cut -d' ' -f2 $PORTS_FILE 2>/dev/null)
	t=$(cut -d' ' -f3 $PORTS_FILE 2>/dev/null)
	[ -n "$p" ] || p=$DEF_PROXY
	[ -n "$w" ] || w=$DEF_WEB
	[ -n "$t" ] || t=$DEF_TPROXY
	echo "$p $w $t"
}

# [orphan-fix] 本插件全部引擎进程。PID 文件会丢/会错(存储卷挂载竞态、升级
# 链路遗留) —— 设备实锤(2026-11-18): 改端口后旧端口的 mitmweb 没被杀掉,
# 18071/17071 同时有两个引擎在监听, 而页面状态显示正常。改为按 bundle 里的
# musl loader 路径扫描 /proc/*/cmdline, 不依赖 PID 文件。
# [perf] 单次 grep -l 扫全部 cmdline(一个进程), 不要逐 PID tr|grep —— 后者
# 2N 个进程, show 数据要慢 ~0.5s, 用户视角"进页面先显示已停止, 等一会才
# 运行中"(v1.0.4 实锤)。正则写 [.] 防 grep 自身命令行自匹配。
__engine_pids()
{
	local f p ids=""
	# [go-rewrite] 新引擎 bins/mitmproxy-go; 兼容旧 Python 引擎(≤1.0.7 的
	# ld-musl loader): 升级时框架先换文件再跑 install.sh, 旧 Python 进程还占着
	# 18070-18072 —— 不按旧模式匹配就杀不掉, 新引擎端口占用起不来(升级路径实锤)。
	local pat="$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/mitmproxy-go"
	local pat_old="$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/runtime/lib/ld-musl-x86_64[.]so[.]1"
	for f in $(grep -lE "$pat|$pat_old" /proc/[0-9]*/cmdline 2>/dev/null); do
		p=${f#/proc/}
		p=${p%/*}
		[ "$p" = "$$" ] && continue
		ids="$ids $p"
	done
	echo $ids
}

# [fd-limit] mitmweb 是 Python 进程, 连接数一样吃 nofile; 启动前把软限制尽量
# 抬高(抬不过硬限制就静默失败, 不影响启动)。reqable 引擎同款兜底。
__raise_nofile()
{
	local n
	for n in 65536 32768 8192 4096; do ulimit -n $n 2>/dev/null && break; done
	return 0
}

start()
{
	if [ -n "$(__engine_pids)" ]; then
		echo "mitmproxy(Go 引擎)已在运行(pid $(__engine_pids | tr -s ' '))"
		return 0
	fi
	if [ ! -x "$BIN" ]; then
		echo "启动失败: Go 引擎不存在或没有可执行权限($BIN)"
		echo "  这通常是插件安装不完整(bundle 约 20MB, 存储分区空间不足会被静默截断)"
		df -h 2>/dev/null | head -10
		return 1
	fi
	mkdir -p $CONFDIR $LOGDIR 2>/dev/null
	local ports=$(__ports)
	local proxy=${ports%% *}
	local rest=${ports#* }
	local web=${rest%% *}
	local tproxy=${rest##* }
	# 端口被别的进程占着要先报出来(否则引擎起不来但看不出原因)
	local i
	for i in $proxy $web $tproxy; do
		if netstat -tln 2>/dev/null | grep -q ":$i "; then
			echo "端口 $i 已被占用, 请到页面改端口或停止占用该端口的程序"
			return 1
		fi
	done
	__raise_nofile
	# [plugin-proxy] WebUI 绑址见 __web_bind_addr: 新固件只绑 127.0.0.1(代理转发),
	# 旧固件 0.0.0.0 直连。两个代理端口保持 0.0.0.0(设备要用)。
	local __web_bind
	__web_bind=$(__web_bind_addr)
	# [go-rewrite] Go 引擎: 显式(regular) + 透明(transparent) 双监听 + API/WebUI;
	# -data-dir 落存储卷(CA/规则/flow 上限都在里面); -transparent 开透明监听。
	# 引擎 WebUI 中文原生 —— 原 Python 版的 lang.json 同步/i18n 注入全部删除。
	$BIN -proxy-addr 0.0.0.0:$proxy -transparent-addr 0.0.0.0:$tproxy \
		-api-addr $__web_bind:$web -data-dir $CONFDIR -transparent \
		>>$LOGFILE 2>&1 &
	echo $! > $PIDFILE
	# [plugin-proxy] 落端口文件: OpenResty /plugin_proxy/mitmproxy/ 代理按插件名
	# 查此文件找本地端口; 只绑 127.0.0.1 时这是唯一入口(直连已拒)
	if [ "$__web_bind" = "127.0.0.1" ]; then
		mkdir -p /tmp/iktmp/plugin_web 2>/dev/null
		echo $web > $PP_PORT_FILE 2>/dev/null
	fi
	# [go-rewrite] 健康检查: Go 引擎毫秒级起(实测 API 首响 ~20ms), 10s 足够;
	# 原 Python 版冷启动要 60s 等待(设备实锤误报"启动异常"的根因)。
	local ok=0 i=0
	while [ $i -lt 10 ]; do
		i=$((i+1))
		if wget -q -O /dev/null -T2 http://127.0.0.1:$web/api/flows 2>/dev/null; then ok=1; break; fi
		[ -n "$(__engine_pids)" ] || break
		sleep 1
	done
	if [ "$ok" = "1" ]; then
		echo "mitmproxy(Go 引擎)已启动: 显式代理 $proxy / 透明 $tproxy / Web $web"
		# [capture-pause] 引擎记录层对齐总开关: 开关关着(无 flag)时引擎必须暂停,
		# 否则"关了抓包再重启引擎"会静默恢复记录(引擎每次启动 paused 默认 false)
		if [ -f $CAPTURE_FLAG ]; then
			wget -q -O /dev/null -T3 --method=PUT --body-data='{"paused":false}' \
				http://127.0.0.1:$web/api/capture 2>/dev/null
		else
			wget -q -O /dev/null -T3 --method=PUT --body-data='{"paused":true}' \
				http://127.0.0.1:$web/api/capture 2>/dev/null
		fi
		# [boot-heal] 开机/重启后规则自愈: 总开关开着就把规则补上
		# (否则重启引擎后 iptables 规则没了, 用户视角"重启后抓不到包")
		[ -f $CAPTURE_FLAG ] && iptables_add >/dev/null 2>&1
		return 0
	fi
	echo "mitmproxy(Go 引擎)启动异常(等了 10 秒 API 没起来), 日志尾部:"
	tail -20 $LOGFILE 2>/dev/null | tail -5
	rm -f $PIDFILE
	return 1
}

stop()
{
	# [plugin-proxy] 停止即摘掉代理端口文件(文件不在=代理明确报"插件未运行")
	rm -f $PP_PORT_FILE
	# [orphan-fix] 杀全部引擎进程(PID 文件可能丢/错, 见 __engine_pids 注释),
	# 不是只杀 PID 文件里那一个 —— 设备实锤: 只杀一个会留下孤儿引擎继续占老端口
	local pids p
	pids=$(__engine_pids)
	for p in $pids; do
		kill $p 2>/dev/null
	done
	# [stop-wait] kill 只是发信号, 进程退出和端口释放是异步的 —— 紧接着
	# start 会 bind 失败(reqable 设备实踩竞态)。轮询等到真消失再 -9 兜底。
	if [ -n "$pids" ]; then
		local i=0
		while [ -n "$(__engine_pids)" ] && [ $i -lt 50 ]; do
			sleep 0.1
			i=$((i+1))
		done
		for p in $(__engine_pids); do kill -9 $p 2>/dev/null; done
	fi
	rm -f $PIDFILE
	# [stop-rules] 停引擎必须同时撤 iptables 规则: 规则指向没监听的端口会把
	# 选中设备的 80/443 全弄断(用户视角"停了插件结果断网")
	iptables_del
	echo "mitmproxy(Go 引擎)已停止"
	return 0
}

# ============ 层2: iptables 规则生成(按设备精确匹配, 幂等) ============
# 从 devices.json 读设备列表, 生成 nat REDIRECT 规则(每设备一条)
# 匹配优先级: MAC 优先(DHCP 换 IP 不失效), 无 MAC 用 -s IP
iptables_add()
{
	local ports="80,443"
	[ -f $DPORTS_FILE ] && ports=$(cat $DPORTS_FILE)
	local devs="[]"
	[ -f $DEVICES_FILE ] && devs=$(cat $DEVICES_FILE)

	local tproxy=$(__ports | cut -d' ' -f3)

	# 链(幂等)。链名带 MITMPROXY 前缀, 与 reqable 插件的 REQABLE_* 互不干扰
	# (两个抓包插件可能同时启用)
	iptables -t nat -N MITMPROXY_REDIRECT 2>/dev/null
	iptables -t nat -C PREROUTING -j MITMPROXY_REDIRECT 2>/dev/null || iptables -t nat -I PREROUTING -j MITMPROXY_REDIRECT
	iptables -t nat -F MITMPROXY_REDIRECT
	iptables -N MITMPROXY_QUIC 2>/dev/null
	iptables -C FORWARD -j MITMPROXY_QUIC 2>/dev/null || iptables -I FORWARD -j MITMPROXY_QUIC
	iptables -F MITMPROXY_QUIC

	# 排除路由器自身全部 IP(否则 LAN 访问路由器管理页也会被截)
	local lip
	for lip in $(ip -o -4 addr show 2>/dev/null | awk '{print $4}' | cut -d/ -f1); do
		iptables -t nat -A MITMPROXY_REDIRECT -d $lip -j RETURN
	done

	# 逐设备生成规则
	local count=0
	while read -r d; do
		[ -z "$d" ] && continue
		local mac=$(echo "$d" | jq -r '.mac // empty' 2>/dev/null)
		local ip=$(echo "$d" | jq -r '.ip // empty' 2>/dev/null)
		local match=""
		[ -n "$mac" ] && [ "$mac" != "null" ] && match="-m mac --mac-source $mac"
		[ -z "$match" ] && [ -n "$ip" ] && [ "$ip" != "null" ] && match="-s $ip"
		[ -z "$match" ] && continue
		iptables -t nat -A MITMPROXY_REDIRECT -p tcp -m multiport --dports $ports $match -j REDIRECT --to-ports $tproxy
		# QUIC(UDP443) 阻断, 强制浏览器回退 TCP(否则 HTTP/3 绕过 MITM)
		iptables -A MITMPROXY_QUIC -p udp --dport 443 $match -j DROP
		count=$((count+1))
	done < <(echo "$devs" | jq -c '.[]' 2>/dev/null)
	touch $RULES_FLAG
	echo "规则已生效: $count 台设备 -> 透明端口 $tproxy"
	return 0
}

iptables_del()
{
	iptables -t nat -D PREROUTING -j MITMPROXY_REDIRECT 2>/dev/null
	iptables -t nat -F MITMPROXY_REDIRECT 2>/dev/null
	iptables -t nat -X MITMPROXY_REDIRECT 2>/dev/null
	iptables -D FORWARD -j MITMPROXY_QUIC 2>/dev/null
	iptables -F MITMPROXY_QUIC 2>/dev/null
	iptables -X MITMPROXY_QUIC 2>/dev/null
	rm -f $RULES_FLAG
	return 0
}

# 端口配置: set_ports proxy=18070 web=18071 tproxy=18072(改了自动重启引擎)
set_ports()
{
	local p=${proxy:-$DEF_PROXY} w=${web:-$DEF_WEB} t=${tproxy:-$DEF_TPROXY}
	local x
	for x in "$p" "$w" "$t"; do
		case "$x" in ''|*[!0-9]*) echo "端口必须是数字: $x"; return 1;; esac
		[ "$x" -ge 1 ] && [ "$x" -le 65535 ] || { echo "端口超出范围: $x"; return 1; }
	done
	if [ "$p" = "$w" ] || [ "$p" = "$t" ] || [ "$w" = "$t" ]; then
		echo "三个端口不能相同"
		return 1
	fi
	mkdir -p $CONFDIR 2>/dev/null
	echo "$p $w $t" > $PORTS_FILE
	# [orphan-fix] 用 /proc 扫描判断引擎在不在跑(PID 文件会丢/错): 在跑才
	# 停+起。以前用 PID 文件, 文件丢时就跳过硬重启, 旧引擎继续占老端口 ——
	# 设备实锤: 改端口后新旧两个引擎同时监听, iframe 连新端口被拒
	local was=0
	[ -n "$(__engine_pids)" ] && was=1
	if [ "$was" = "1" ]; then
		stop >/dev/null 2>&1
		start >/dev/null 2>&1 || { echo "端口已保存, 但引擎重启失败, 请手动启动"; return 1; }
		# 规则按新透明端口热更新(总开关开着才重建)
		[ -f $CAPTURE_FLAG ] && iptables_add >/dev/null 2>&1
	fi
	echo "端口已保存: 显式代理 $p / Web $w / 透明 $t"
	return 0
}

# 界面语言: set_lang lang=zh|en(写 LANG_FILE, 页面 show&TYPE=data 带回)
# [go-rewrite] Go 引擎 WebUI 中文原生, 不再需要 lang.json 同步 —— 此开关只影响
# 插件管理页自身的文案。不重启引擎 —— 重启会断进行中的抓包会话。
set_lang()
{
	case "$lang" in
		zh|en) ;;
		*) echo "lang 参数只能是 zh 或 en"; return 1;;
	esac
	mkdir -p $CONFDIR 2>/dev/null
	echo "$lang" > $LANG_FILE
	echo "界面语言已切换: $lang"
	return 0
}

# 透明拦截端口: set_dports ports='80,443'
set_dports()
{
	echo "$ports" | grep -qE '^[0-9]+(,[0-9]+)*$' || { echo "ports 格式: 80,443"; return 1; }
	echo "$ports" > $DPORTS_FILE
	[ -f $CAPTURE_FLAG ] && iptables_add >/dev/null 2>&1
	echo "拦截端口已更新: $ports"
	return 0
}

# 开机自启: set_auto_start autostart=true|false
set_auto_start()
{
	mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME 2>/dev/null
	if [ "$autostart" = "true" ]; then
		touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
		echo "已开启开机自启"
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
		echo "已关闭开机自启"
	fi
	return 0
}

# 抓包总开关: set_capture capture=true|false
# 开着: 引擎没跑先拉起来(规则指向没监听的端口会把勾选设备的 80/443 全弄断),
#       再上 iptables 规则; 关着: 撤规则。
set_capture()
{
	if [ "$capture" = "true" ]; then
		if [ -z "$(__engine_pids)" ]; then
			start || { echo "引擎未运行且启动失败, 抓包无法开启"; return 1; }
		fi
		touch $CAPTURE_FLAG
		iptables_add
	else
		rm -f $CAPTURE_FLAG
		iptables_del
		echo "抓包已关闭, 网络规则已撤"
	fi
	return 0
}

# [param-b64] 框架 /Action/call 的参数值禁含 引号/空格/$/&/;/|/\ (ikngx.lua 的
# __rest_check_illegality_char), 明文 JSON 必被 2005 拒掉 —— 设备列表一律 base64。
__param_decode()
{
	local v="${!1}"
	if [ -n "$v" ]; then
		echo "$v" | base64 -d 2>/dev/null && return
	fi
	echo "${!2}"
}

# 设备选择: set_devices devices='[{"ip":"..","mac":"..","name":".."}]'
set_devices()
{
	local dj=$(__param_decode devices_b64 devices)
	echo "$dj" | jq -e 'type == "array"' >/dev/null 2>&1 || { echo "devices 参数必须是 JSON 数组"; return 1; }
	echo "$dj" | jq '.' > $DEVICES_FILE
	# 规则热更新(总开关开着才重建)
	if [ -f $CAPTURE_FLAG ]; then
		iptables_add
	fi
	local n=$(jq 'length' $DEVICES_FILE)
	echo "已更新设备列表($n 台)"
	return 0
}

# 清空引擎里的流量(DELETE /api/flows)
clear_flows()
{
	local ports=$(__ports)
	local rest=${ports#* }
	local web=${rest%% *}
	wget -q -O /dev/null -T3 --method=DELETE http://127.0.0.1:$web/api/flows 2>/dev/null
	echo "已清空"
	return 0
}

# [CA通道] 页面是 HTTPS(路由器 Web UI), 直链 http://IP:18071/... 下载 CA 会被
# 浏览器当混合内容拦掉(现象: 点了一闪而过没下载)。改走同源的 show&TYPE=ca:
# CA 内容经本接口 base64 回传, 页面端 Blob 下载, 全程同源。
__show_ca()
{
	local f="" c
	for c in $CONFDIR/mitmproxy-ca-cert.pem $CONFDIR/mitmproxy-ca-cert.cer; do
		[ -s "$c" ] && f="$c" && break
	done
	if [ -z "$f" ]; then
		__json_result__='{"ca_b64":""}'
		return 0
	fi
	local b64=$(base64 "$f" 2>/dev/null | tr -d '\n')
	__json_result__="{\"ca_b64\":\"$b64\"}"
	return 0
}

# [netdiag] 网络层诊断: iptables 规则/设备表/引擎日志一次带回。
# 排查"设备勾选了却断网+无流量"类问题: 规则没匹配上 / 引擎隧道失败。
# [netdiag-json] 所有字段必须 tr 掉裸换行再拼 JSON 字符串 —— reqable 实测教训:
# devices.json 原文含裸换行, 拼进去整个 /Action/call 响应就是非法 JSON,
# 前端 JSON.parse 直接抛异常, 诊断面板整个打不开。
__show_netdiag()
{
	local nat_rules fwd_rules devs logtail lanip
	nat_rules=$(iptables -t nat -S MITMPROXY_REDIRECT 2>&1 | tr '\n' ';' | tr -d '"\\\r	')
	nat_rules="$nat_rules PREROUTING: $(iptables -t nat -S PREROUTING 2>&1 | grep MITMPROXY | tr '\n' ',')"
	fwd_rules=$(iptables -S MITMPROXY_QUIC 2>&1 | tr '\n' ';' | tr -d '"\\\r	')
	fwd_rules="$fwd_rules FORWARD: $(iptables -S FORWARD 2>&1 | grep MITMPROXY | tr '\n' ',')"
	devs=$(cat $DEVICES_FILE 2>/dev/null | tr -d '"\\\n\r	' | head -c 400)
	logtail=$(tail -12 $LOGFILE 2>/dev/null | tr '\n' '|' | tr -d '"\\\r	')
	lanip=$(ip route 2>/dev/null | awk '/src/ {for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
	__json_result__="{\"mark\":\"netdiag\",\"nat\":\"$nat_rules\",\"filter\":\"$fwd_rules\",\"devices\":\"$devs\",\"logtail\":\"$logtail\",\"lanip\":\"$lanip\",\"rules_flag\":\"$([ -f $RULES_FLAG ] && echo on || echo off)\",\"capture\":\"$([ -f $CAPTURE_FLAG ] && echo on || echo off)\"}"
	return 0
}

__show_data()
{
	local status=0
	# [orphan-fix] 状态以 /proc 扫描为准: PID 文件丢/错时旧代码显示"已停止",
	# 实际引擎还在跑(设备实锤), 用户点停止杀不掉, 点启动报端口占用
	[ -n "$(__engine_pids)" ] && status=1
	local ports=$(__ports)
	local proxyPort=${ports%% *}
	local rest=${ports#* }
	local webPort=${rest%% *}
	local transparentPort=${rest##* }
	local autostart=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1
	local caok=0
	{ [ -s $CONFDIR/ca.pem ] || [ -s $CONFDIR/mitmproxy-ca-cert.pem ]; } && caok=1
	local flows=0
	if [ "$status" = "1" ]; then
		flows=$(wget -q -O - -T3 http://127.0.0.1:$webPort/api/flows 2>/dev/null | jq '.flows | length' 2>/dev/null)
		[ -z "$flows" ] && flows=0
	fi
	local capture=0
	[ -f $CAPTURE_FLAG ] && capture=1
	local rulesOn=0
	[ -f $RULES_FLAG ] && rulesOn=1
	local devices='[]'
	[ -f $DEVICES_FILE ] && devices=$(cat $DEVICES_FILE 2>/dev/null | tr -d '\n\r\t')
	local dports='80,443'
	[ -f $DPORTS_FILE ] && dports=$(cat $DPORTS_FILE)
	local lang=zh
	[ -f $LANG_FILE ] && lang=$(cat $LANG_FILE 2>/dev/null | tr -d '\n\r')
	case "$lang" in zh|en) ;; *) lang=zh;; esac
	# [plugin-proxy] 页面按此决定 iframe 走同源代理路径还是直连(见 __web_bind_addr)
	local proxyReady=0
	[ "$(__web_bind_addr)" = "127.0.0.1" ] && proxyReady=1
	json_append __json_result__ status:int proxyPort:int webPort:int transparentPort:int autostart:int caok:int flows:int capture:int rulesOn:int
	json_append __json_result__ devices:json dports:str lang:str proxyReady:int
	return 0
}

# Show 由 /etc/ikcommon 提供: 按 TYPE 分发 __show_<TYPE>(data/ca/netdiag)
show()
{
	Show __json_result__
}
