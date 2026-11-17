#!/bin/bash /etc/ikcommon
# Mitmproxy 抓包插件 service.sh
# 架构: 层1 显式代理(客户端手动设代理 router_ip:18070, v1 不做 iptables 透明引流)
#       层2 mitmproxy 自身 MITM 解密 + addons
#       层3 mitmweb Web 界面(18071)查看/改写/断点
# 运行时: 自带 Alpine(musl) Python3.14 + mitmproxy 11(bins/runtime), 经自带 musl
#         loader 启动 —— 固件是 uClibc 且无 Python, 与宿主 libc 完全无关。
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
if [ -n "$__pdir" ] && [ -f "$__pdir/bins/runtime/run-mitmweb.sh" ]; then
	[ -n "$__idir" ] && [ "$__idir" != "$EXT_PLUGIN_INSTALL_DIR" ] && EXT_PLUGIN_INSTALL_DIR="$__idir"
	[ -f "$__pdir/../plugins" ] || EXT_PLUGIN_IPK_DIR="$__pdir/../plugins"
fi
unset __slf __slfdir __pdir __idir

RUNNER=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/runtime/run-mitmweb.sh
# [storage-anchor] CA/配置/pid 全落配置目录(存储卷; 未设存储时 p5, 绝不落 sda3)
CONFDIR=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
PIDFILE=$CONFDIR/mitmweb.pid
LOGDIR=$EXT_PLUGIN_LOG_DIR
LOGFILE=$LOGDIR/mitmproxy.log
DEF_PROXY=18070
DEF_WEB=18071
PORTS_FILE=$CONFDIR/ports

# 读端口配置(文件格式: "代理端口 Web端口"); 没配过用默认值
__ports()
{
	local p w
	p=$(cut -d' ' -f1 $PORTS_FILE 2>/dev/null)
	w=$(cut -d' ' -f2 $PORTS_FILE 2>/dev/null)
	[ -n "$p" ] || p=$DEF_PROXY
	[ -n "$w" ] || w=$DEF_WEB
	echo "$p $w"
}

__engine_pid()
{
	[ -f $PIDFILE ] || return 1
	local pid=$(cat $PIDFILE 2>/dev/null)
	[ -n "$pid" ] || return 1
	kill -0 $pid 2>/dev/null || return 1
	echo $pid
}

start()
{
	if __engine_pid >/dev/null; then
		echo "mitmweb 已在运行(pid $(cat $PIDFILE))"
		return 0
	fi
	if [ ! -x "$RUNNER" ]; then
		echo "启动失败: 运行时不存在或没有可执行权限($RUNNER)"
		echo "  这通常是插件安装不完整(bundle 约 100MB, 存储分区空间不足会被静默截断)"
		df -h 2>/dev/null | head -10
		return 1
	fi
	mkdir -p $CONFDIR $LOGDIR 2>/dev/null
	local ports=$(__ports)
	local proxy=${ports%% *} web=${ports##* }
	# 端口被别的进程占着要先报出来(否则 mitmweb 起不来但看不出原因)
	local i
	for i in $proxy $web; do
		if netstat -tln 2>/dev/null | grep -q ":$i "; then
			echo "端口 $i 已被占用, 请到页面改端口或停止占用该端口的程序"
			return 1
		fi
	done
	# [musl-loader] 显式代理 + mitmweb; confdir 落存储卷(CA/配置); 不弹浏览器
	# web_open_browser=false: 路由器上没有浏览器
	$RUNNER --listen-host 0.0.0.0 --listen-port $proxy \
		--web-host 0.0.0.0 --web-port $web \
		--set confdir=$CONFDIR --set web_open_browser=false \
		>>$LOGFILE 2>&1 &
	echo $! > $PIDFILE
	# 健康检查: Python 启动要几秒, 轮询 web 端口(最多 ~15s)
	local ok=0
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
		sleep 1
		if wget -q -O /dev/null -T2 http://127.0.0.1:$web/ 2>/dev/null; then ok=1; break; fi
		__engine_pid >/dev/null || break
	done
	if [ "$ok" = "1" ]; then
		echo "mitmweb 已启动: 代理 $proxy / Web $web"
		return 0
	fi
	echo "mitmweb 启动异常(等了 15 秒 Web 端口没起来), 日志尾部:"
	tail -5 $LOGFILE 2>/dev/null
	rm -f $PIDFILE
	return 1
}

stop()
{
	local pid=$(__engine_pid)
	if [ -n "$pid" ]; then
		kill $pid 2>/dev/null
		sleep 1
		kill -0 $pid 2>/dev/null && kill -9 $pid 2>/dev/null
	fi
	rm -f $PIDFILE
	echo "mitmweb 已停止"
	return 0
}

# 端口配置: set_ports proxy=18070 web=18071(改了自动重启引擎)
set_ports()
{
	local p=${proxy:-$DEF_PROXY} w=${web:-$DEF_WEB}
	case "$p" in ''|*[!0-9]*) echo "代理端口必须是数字: $p"; return 1;; esac
	case "$w" in ''|*[!0-9]*) echo "Web 端口必须是数字: $w"; return 1;; esac
	[ "$p" -ge 1 ] && [ "$p" -le 65535 ] || { echo "代理端口超出范围: $p"; return 1; }
	[ "$w" -ge 1 ] && [ "$w" -le 65535 ] || { echo "Web 端口超出范围: $w"; return 1; }
	[ "$p" != "$w" ] || { echo "代理端口与 Web 端口不能相同"; return 1; }
	mkdir -p $CONFDIR 2>/dev/null
	echo "$p $w" > $PORTS_FILE
	local was=0
	__engine_pid >/dev/null && was=1
	if [ "$was" = "1" ]; then
		stop >/dev/null 2>&1
		start >/dev/null 2>&1 || { echo "端口已保存, 但引擎重启失败, 请手动启动"; return 1; }
	fi
	echo "端口已保存: 代理 $p / Web $w"
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

# 清空 mitmweb 里的流量(POST /clear)
clear_flows()
{
	local ports=$(__ports)
	local web=${ports##* }
	wget -q -O /dev/null -T3 --method=POST http://127.0.0.1:$web/clear 2>/dev/null
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

__show_data()
{
	local status=0
	__engine_pid >/dev/null && status=1
	local ports=$(__ports)
	local proxyPort=${ports%% *}
	local webPort=${ports##* }
	local autostart=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1
	local caok=0
	{ [ -s $CONFDIR/mitmproxy-ca-cert.pem ] || [ -s $CONFDIR/mitmproxy-ca-cert.cer ]; } && caok=1
	local flows=0
	if [ "$status" = "1" ]; then
		flows=$(wget -q -O - -T3 http://127.0.0.1:$webPort/flows 2>/dev/null | jq 'length' 2>/dev/null)
		[ -z "$flows" ] && flows=0
	fi
	local uiUrl="http://$(cat /etc/hostname 2>/dev/null):$webPort/"
	json_append __json_result__ status:int proxyPort:int webPort:int autostart:int caok:int flows:int
	return 0
}

show()
{
	Show __json_result__
}
