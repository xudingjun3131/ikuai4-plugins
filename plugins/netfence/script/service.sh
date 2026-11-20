#!/bin/bash /etc/ikcommon
# NetFence 一体化防火墙 · 插件服务脚本
# 契约: 经 /usr/ikuai/function/plugin_netfence 软链被 ikcommon 调用;
# 动作函数由框架的 Command 分发, 参数以局部变量形式注入。
# 所有配置/日志/列表落存储卷 $EXT_PLUGIN_CONFIG_DIR/netfence, 不落 sda3。
PLUGIN_NAME="netfence"
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

# [self-locate] v1.0.1 真机 P1 修复(测试报告 TEST-REPORT-2026-11-20-netfence):
# 框架是经 /usr/ikuai/function/plugin_netfence 软链调用本脚本的, $0 是软链路径。
# 旧版直接用 dirname "$0" -> INSTALL_DIR 落到 /usr/ikuai/function, BIN 指向不存在的
# /usr/ikuai/bin/netfenced -> start 的 __bin_check 失败返回 1(响应空)、show 读不到
# 版本回 unknown、按钮文案不变。三个现象互锁。修法照同仓库 runhooks 的
# [self-locate]: 先 readlink 解析软链再取目录。
# 目录关系: <EXT_PLUGIN_INSTALL_DIR>/<PLUGIN_NAME>/script/service.sh
#           退一层 = 插件目录, 退两层 = EXT_PLUGIN_INSTALL_DIR
# 包内二进制在 <插件目录>/bin/netfenced (metadata.bin 映射, 源码侧是 bins/netfence-x86_64)。
__slf="$0"
if [ -L "$__slf" ]; then
    __t=$(readlink "$__slf" 2>/dev/null)
    if [ -n "$__t" ]; then
        case "$__t" in
            /*) __slf="$__t" ;;
            *)  __slf="$(dirname "$0")/$__t" ;;
        esac
    fi
fi
[ -n "$__slf" ] || __slf="$0"
__slfdir=$(cd "$(dirname "$__slf")" 2>/dev/null && pwd)
__pdir=$(cd "$__slfdir/.." 2>/dev/null && pwd)       # 插件目录
__idir=$(cd "$__slfdir/../.." 2>/dev/null && pwd)    # EXT_PLUGIN_INSTALL_DIR
if [ -n "$__pdir" ] && [ -f "$__pdir/bin/netfenced" ]; then
    [ -n "$__idir" ] && [ "$__idir" != "$EXT_PLUGIN_INSTALL_DIR" ] && EXT_PLUGIN_INSTALL_DIR="$__idir"
fi
INSTALL_DIR="$__pdir"
unset __slf __t __slfdir __pdir __idir

# 不同固件 ikcommon 传给脚本的 $0 形态不完全一致(有的给软链路径、有的给
# /etc/ikcommon 自身)。逐候选探测真实二进制位置, 命中即用, 不再单点押注 $0。
BIN=""
for __cand in \
    "$INSTALL_DIR/bin/netfenced" \
    "$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/netfenced" \
    "$(dirname "$0")/../bin/netfenced"; do
    if [ -f "$__cand" ]; then BIN="$__cand"; break; fi
done
[ -n "$BIN" ] || BIN="$INSTALL_DIR/bin/netfenced"
unset __cand

# 旧固件(无 helper)兜底, 避免路径拼到 / 根目录
[ -n "$EXT_PLUGIN_CONFIG_DIR" ] || EXT_PLUGIN_CONFIG_DIR="$INSTALL_DIR/../.netfence_config"
DATA_DIR="$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME"
[ -n "$EXT_PLUGIN_LOG_DIR" ] || EXT_PLUGIN_LOG_DIR="$DATA_DIR"
PIDFILE="$DATA_DIR/netfenced.pid"
LOGFILE="$EXT_PLUGIN_LOG_DIR/$PLUGIN_NAME.log"
PORTS_CONF="$DATA_DIR/ports.conf"
API_PORT=18111
DNS_PORT=18110
UPSTREAM="223.5.5.5:53"

[ -f "$PORTS_CONF" ] && . "$PORTS_CONF"

# [plugin-proxy] v1.0.1: 固件 OpenResty 按插件名查此文件找本地端口。
# 旧版没写这个文件, 引擎起来后 /plugin_proxy/netfence/* 仍 502
# ("plugin web ui not available: netfence"), 页面所有 API 都拿不到。
# 新固件(Build202611181800+)引擎只绑 127.0.0.1 走同源代理; 旧固件绑 0.0.0.0 直连。
__PROXY_MIN_BUILD=202611181800
PP_PORT_FILE=/tmp/iktmp/plugin_web/$PLUGIN_NAME
__web_bind_addr()
{
    local __bd
    __bd=$(grep '^BUILD_DATE=' /etc/release 2>/dev/null | cut -d= -f2)
    case "$__bd" in ''|*[!0-9]*) echo 0.0.0.0; return ;; esac
    if [ "$__bd" -ge "$__PROXY_MIN_BUILD" ]; then echo 127.0.0.1; else echo 0.0.0.0; fi
}
__write_port_file()
{
    # 无论绑 127.0.0.1(新固件同源代理)还是 0.0.0.0(旧固件直连), 一律写端口文件:
    # 新固件 OpenResty 靠它转发, 旧固件没有该代理、写了也无人读, 无副作用。
    # 这样不依赖 BUILD_DATE 判定就能保证新固件上 /plugin_proxy/netfence/* 可达。
    mkdir -p "$(dirname "$PP_PORT_FILE")" 2>/dev/null
    echo "$API_PORT" > "$PP_PORT_FILE" 2>/dev/null
}

mkdir -p "$DATA_DIR/lists" "$DATA_DIR/threat" 2>/dev/null

# [jq-shim] 设备固件是裁剪版 jq, --argjson 恒返回空。所有 JSON 构造只用
# printf 与纯文本拼接, 不依赖 jq 的高级选项。
__json_escape() {
    printf '%s' "$1" | tr -d '\n\r' | sed 's/\\/_/g; s/"/_/g'
}

__alive() {
    [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null
}

# [diag-to-stdout] 框架对非 show 动作走 Exec_Return: 成功时 stdout 被整体丢弃,
# 失败(返回非 0)才把 stdout 原样回传。所以失败原因必须 echo 到 stdout 而不是
# stderr(stderr 被 2>/dev/null 吞掉, 现场表现就是"响应体仅一个换行"查不到原因)。
__bin_check() {
    if [ ! -f "$BIN" ] || [ ! -x "$BIN" ]; then
        echo "引擎二进制缺失或不可执行: $BIN (self-locate argv0=$0)" >&1
        echo "$(date) __bin_check FAIL BIN=$BIN argv0=$0" >>"$LOGFILE" 2>/dev/null
        return 1
    fi
    return 0
}

start()
{
    __bin_check || return 1
    if __alive; then
        __write_port_file     # 端口文件被清 /tmp 后自愈, 不重启引擎
        return 0
    fi
    local __web_bind
    __web_bind=$(__web_bind_addr)
    local dns_arg=""
    [ -n "$DNS_PORT" ] && [ "${DNS_ENABLE:-0}" = "1" ] && dns_arg="-dns 0.0.0.0:$DNS_PORT"
    # 照 runhooks: 直接后台拉起 + stdout/stderr 全重定向到日志 + stdin /dev/null,
    # 避免子进程继承框架命令替换的管道导致调用挂住; 不依赖 pgrep/start-stop-daemon。
    "$BIN" -api "$__web_bind:$API_PORT" -config "$DATA_DIR" -upstream "$UPSTREAM" \
        $dns_arg >>"$LOGFILE" 2>&1 </dev/null &
    echo $! > "$PIDFILE"
    __write_port_file "$__web_bind"
    # 轮询等进程真正存活, 不押注后台立即就绪
    local i=0
    while [ $i -lt 20 ]; do
        if __alive; then return 0; fi
        i=$((i+1))
        sleep 0.2
    done
    echo "引擎启动失败: 进程未存活, 详见 $LOGFILE" >&1
    rm -f "$PP_PORT_FILE"
    return 1
}

stop()
{
    # [plugin-proxy] 停止即摘掉端口文件: 文件在=代理可转发, 不在=明确报插件未运行
    rm -f "$PP_PORT_FILE"
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null
    fi
    # [stop-wait] 不等进程退出就紧接着 start 会 bind 失败: 轮询到真消失
    local i=0
    while [ $i -lt 25 ] && __alive; do
        i=$((i+1))
        sleep 0.2
    done
    __alive && kill -9 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null
    rm -f "$PIDFILE"
    return 0
}

restart()
{
    stop
    start
}

set_auto_start()
{
    local on="${enabled:-${autostart:-0}}"
    if [ "$on" = "1" ] || [ "$on" = "true" ]; then
        echo 1 > "$DATA_DIR/autostart"
    else
        rm -f "$DATA_DIR/autostart"
    fi
    return 0
}

set_ports()
{
    # [ports-reread] 写完必须立即重读, 否则本次 start 用的还是脚本加载时的旧值
    [ -n "$apiPort" ] && API_PORT="$apiPort"
    [ -n "$dnsPort" ] && DNS_PORT="$dnsPort"
    [ -n "$upstream" ] && UPSTREAM="$upstream"
    [ -n "$dnsEnable" ] && DNS_ENABLE="$dnsEnable"
    printf 'API_PORT=%s\nDNS_PORT=%s\nUPSTREAM=%s\nDNS_ENABLE=%s\n' \
        "$API_PORT" "$DNS_PORT" "$UPSTREAM" "${DNS_ENABLE:-0}" > "$PORTS_CONF"
    . "$PORTS_CONF"
    restart
    return 0
}

reload()
{
    # SIGHUP 热重载策略/列表, 不重启进程、不断流量
    if [ -f "$PIDFILE" ]; then
        kill -HUP "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null
    fi
    return 0
}

__show_data()
{
    local status=0
    __alive && status=1
    local autostart=0
    [ -f "$DATA_DIR/autostart" ] && autostart=1
    local dnsEnable=0
    [ "${DNS_ENABLE:-0}" = "1" ] && dnsEnable=1
    local version
    version=$("$BIN" -version 2>/dev/null | head -1)
    [ -z "$version" ] && version="unknown"

    # [busybox-wget] 设备是裁剪版 busybox wget, 用 -T2 而不是 GNU 的 --timeout=2
    local api_up=0 blocked=0 total=0 rules=0 lists=0 threat=0
    local health
    health=$(wget -q -O - -T2 "http://127.0.0.1:$API_PORT/api/health" 2>/dev/null)
    [ -n "$health" ] && api_up=1
    local summ
    summ=$(wget -q -O - -T2 "http://127.0.0.1:$API_PORT/api/summary" 2>/dev/null)
    if [ -n "$summ" ]; then
        blocked=$(printf '%s' "$summ" | sed -n 's/.*"blocked":\([0-9]*\).*/\1/p')
        total=$(printf '%s' "$summ" | sed -n 's/.*"total":\([0-9]*\).*/\1/p')
        [ -z "$blocked" ] && blocked=0
        [ -z "$total" ] && total=0
    fi
    local stat
    stat=$(wget -q -O - -T2 "http://127.0.0.1:$API_PORT/api/status" 2>/dev/null)
    if [ -n "$stat" ]; then
        rules=$(printf '%s' "$stat" | sed -n 's/.*"policy_rules":\([0-9]*\).*/\1/p')
        lists=$(printf '%s' "$stat" | sed -n 's/.*"block":\([0-9]*\).*/\1/p')
        threat=$(printf '%s' "$stat" | sed -n 's/.*"ips":\([0-9]*\).*/\1/p')
        [ -z "$rules" ] && rules=0
        [ -z "$lists" ] && lists=0
        [ -z "$threat" ] && threat=0
    fi

    local v
    v=$(__json_escape "$version")
    __json_result__="{\"status\":$status,\"autostart\":$autostart,\"dnsEnable\":$dnsEnable,\"apiPort\":$API_PORT,\"dnsPort\":$DNS_PORT,\"version\":\"$v\",\"apiUp\":$api_up,\"blocked\":$blocked,\"total\":$total,\"rules\":$rules,\"lists\":$lists,\"threat\":$threat}"
    return 0
}

show()
{
    Show __json_result__
}
