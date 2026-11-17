#!/bin/bash /etc/ikcommon
PLUGIN_NAME="frpc"
. /etc/mnt/plugins/configs/config.sh

if [ ! -f /usr/bin/frpc ]; then
    chmod +x $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/frpc
    ln -fs $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/frpc /usr/bin/frpc
fi

# 读取某个键的值
# 参数: section 键名
CONFIG_FILE="$EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini"
# [config-relocate] 框架 set_storage 会把 EXT_PLUGIN_CONFIG_DIR 整体迁到存储路径,
# 此文件必须跟着变量走 —— 旧版硬编码 /etc/mnt/frpc.config 写在配置分区(sda3), 不随存储走
FRPC_ECHO="$EXT_PLUGIN_CONFIG_DIR/frpc/frpc.config"
read_ini_value() {
    local section=$1 key=$2
    awk -F' *= *' -v section="[$section]" -v key="$key" '
        $1 == section {found=1; next}
        found && $1 == key {print $2; exit}
        /^\[.*\]/ {found=0}
    ' "$CONFIG_FILE"
}

# 修改某个键的值
# 参数: section 键名 值
update_ini_value() {
    local section=$1
    local key=$2
    local value=$3
    sed -i "/\[$section\]/,/^$/s/^$key.*/$key = $value/" "$CONFIG_FILE"
    echo "$key 已更新为 $value"
}

start() {

    if [ -f /tmp/frpc_file.log ]; then
        return
    fi

    if killall -q -0 frpc; then
        killall frpc
        return
    fi

    if [ ! -f $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini ]; then
        return
    fi

    if [ ! -f /usr/bin/frpc ]; then
        filedon
    fi

    sed -i '/localPath/d' $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini
    sed -i 's|log_file = .*|log_file = /tmp/log/frpc.log|' $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini

    ln -s $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini /usr/ikuai/www/frpc.txt

    # [4.x web栈适配] start-stop-daemon -b 守护化+stdio重定向:
    # 裸 & 会让守护进程继承 func 的 $(...) 管道 → 阻塞60s超时 → SIGPIPE 打死
    start-stop-daemon -S -x /usr/bin/frpc -b -- -c $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini >/dev/null 2>&1

}

stop() {
    killall frpc
}

disable() {
    killall frpc
    rm $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini
}

update_config() {
    local server="$1"
    local vkey="$2"
    local password="$3"
    local target="$4"
    local local_type="$5"

    server=$(echo "$server" | sed 's/%20/-/g')
    vkey=$(echo "$vkey" | sed 's/%20/-/g')
    local_type=$(echo "$local_type" | sed 's/%20/-/g')
    target=$(echo "$target" | sed 's/%20/-/g')

    mkdir -p "$EXT_PLUGIN_CONFIG_DIR/frpc" 2>/dev/null
    echo "${server}" >"$FRPC_ECHO"
    echo "${vkey}" >>"$FRPC_ECHO"
    echo "${password}" >>"$FRPC_ECHO"
    echo "${target}" >>"$FRPC_ECHO"
    echo "${local_type}" >>"$FRPC_ECHO"

    echo "配置文件已更新："
    cat "$FRPC_ECHO"
    if killall -q -0 frpc; then
        killall frpc
    fi
    start
}

config() {

    if [ -f /tmp/iktmp/import/file ]; then
        filesize=$(stat -c%s "/tmp/iktmp/import/file")
        echo "$filesize" >>/tmp/frpcconfig.log
        if [ $filesize -lt 524288 ]; then

            rm $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini
            mv /tmp/iktmp/import/file $EXT_PLUGIN_CONFIG_DIR/frpc/frpc.ini
            echo "ok" >>/tmp/frpcconfig.log
            killall frpc
            start

        fi

    fi

}

__show_data() {
    local status=0
    PID=$(pidof frpc | awk '{print $NF}')
    [ -n "$PID" ] && status=1
    [ -f /tmp/iktmp/plugins/frpc_installed ] || status=2

    local autostart=0
    [ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1

    local version=$(jq -r '.version' $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/html/metadata.json 2>/dev/null)
    local server_addr="" server_port=""
    [ -f "$CONFIG_FILE" ] && server_addr=$(read_ini_value "common" "server_addr")
    [ -f "$CONFIG_FILE" ] && server_port=$(read_ini_value "common" "server_port")

    json_append __json_result__ autostart:int
    json_append __json_result__ status:int
    json_append __json_result__ version:str
    json_append __json_result__ server_addr:str
    json_append __json_result__ server_port:str
}

show() {
    Show __json_result__
}

__show_status() {
    if killall -q -0 frpc; then
        local status=1
    else
        local status=0
    fi

    if [ ! -f /usr/bin/frpc ]; then
        local status=3
    fi

    if [ -f /tmp/frpc_file.log ]; then
        local status=2
    fi
    json_append __json_result__ status:int
}

__show_config() {

    if [ ! -f /usr/ikuai/www/frpc.txt ]; then
        ln -s $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/script/frpc.ini /usr/ikuai/www/frpc.txt
    fi

    local server_addr=$(read_ini_value "common" "server_addr")
    local server_port=$(read_ini_value "common" "server_port")
    local admin_port=$(read_ini_value "common" "admin_port")

    if [ ! -f /tmp/frpc.version ]; then
        frpc -v >/tmp/frpc.version
    fi
    version=$(cat /tmp/frpc.version)

    json_append __json_result__ server_addr:str
    json_append __json_result__ server_port:str
    json_append __json_result__ admin_addr:str
    json_append __json_result__ admin_user:str
    json_append __json_result__ admin_pwd:str
    json_append __json_result__ admin_port:str
    json_append __json_result__ version:str

}

case "$1" in
start)
    start
    ;;
stop)
    stop
    ;;
*) ;;
esac
