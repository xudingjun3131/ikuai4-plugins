#!/bin/bash /etc/ikcommon
# NetFence 一体化防火墙 · 插件服务脚本
# 契约: 经 /usr/ikuai/function/plugin_netfence 软链被 ikcommon 调用;
# 动作函数由框架的 Command 分发, 参数以局部变量形式注入。
# 所有配置/日志/列表落存储卷 $EXT_PLUGIN_CONFIG_DIR/netfence, 不落 sda3。
PLUGIN_NAME="netfence"
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
DATA_DIR="$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME"
BIN="$INSTALL_DIR/../bin/netfenced"
PIDFILE="$DATA_DIR/netfenced.pid"
LOGFILE="$EXT_PLUGIN_LOG_DIR/$PLUGIN_NAME.log"
PORTS_CONF="$DATA_DIR/ports.conf"
API_PORT=18111
DNS_PORT=18110
UPSTREAM="223.5.5.5:53"

[ -f "$PORTS_CONF" ] && . "$PORTS_CONF"

mkdir -p "$DATA_DIR/lists" "$DATA_DIR/threat" 2>/dev/null

# [jq-shim] 设备固件是裁剪版 jq, --argjson 恒返回空。所有 JSON 构造只用
# printf 与纯文本拼接, 不依赖 jq 的高级选项。
__json_escape() {
    printf '%s' "$1" | tr -d '\n\r' | sed 's/\\/_/g; s/"/_/g'
}

__alive() {
    [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null
}

__bin_check() {
    if [ ! -f "$BIN" ] || [ ! -x "$BIN" ]; then
        echo "引擎二进制缺失或不可执行: $BIN" >&2
        return 1
    fi
    return 0
}

start()
{
    __bin_check || return 1
    if __alive; then
        return 0
    fi
    # [stale-proc] 升级后旧进程仍跑旧 inode: 若端口已被无名 PID 占用, 先清
    local dns_arg=""
    [ -n "$DNS_PORT" ] && [ "$DNS_ENABLE" = "1" ] && dns_arg="-dns 0.0.0.0:$DNS_PORT"
    start-stop-daemon -S -x "$BIN" -b -- \
        -api "127.0.0.1:$API_PORT" \
        -config "$DATA_DIR" \
        -upstream "$UPSTREAM" \
        $dns_arg >>"$LOGFILE" 2>&1
    # 轮询等进程真正存活, 不押注 start-stop-daemon 立即返回
    local i=0
    while [ $i -lt 20 ]; do
        local pid
        pid=$(pgrep -f "$BIN" 2>/dev/null | head -1)
        if [ -n "$pid" ]; then
            echo "$pid" > "$PIDFILE"
            return 0
        fi
        i=$((i+1))
        sleep 0.2
    done
    echo "引擎启动失败, 详见 $LOGFILE" >&2
    return 1
}

stop()
{
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
    # 框架注入: enabled=1|0 (具体变量名由 ikcommon 传入, 兼容两种)
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

    # 引擎 API 汇总(仅本机, 失败不拖垮 show)
    local api_up=0 blocked=0 total=0 rules=0 lists=0 threat=0
    local health
    health=$(wget -q -O - --timeout=2 "http://127.0.0.1:$API_PORT/api/health" 2>/dev/null)
    [ -n "$health" ] && api_up=1
    local summ
    summ=$(wget -q -O - --timeout=2 "http://127.0.0.1:$API_PORT/api/summary" 2>/dev/null)
    if [ -n "$summ" ]; then
        blocked=$(printf '%s' "$summ" | sed -n 's/.*"blocked":\([0-9]*\).*/\1/p')
        total=$(printf '%s' "$summ" | sed -n 's/.*"total":\([0-9]*\).*/\1/p')
        [ -z "$blocked" ] && blocked=0
        [ -z "$total" ] && total=0
    fi
    local stat
    stat=$(wget -q -O - --timeout=2 "http://127.0.0.1:$API_PORT/api/status" 2>/dev/null)
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
