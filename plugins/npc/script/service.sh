#!/bin/bash /etc/ikcommon
PLUGIN_NAME="npc"
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

if [ ! -f /usr/sbin/npc ]; then
    chmod +x $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/npc
    ln -fs $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/npc /usr/sbin/npc
fi

start() {
    if killall -q -0 npc; then
        killall npc
        return
    fi

    if [ -f $EXT_PLUGIN_CONFIG_DIR/npc/npc.config ]; then
        . $EXT_PLUGIN_CONFIG_DIR/npc/npc.config
        target=$(echo "$target" | sed 's/-/ /g') # 将 - 转换回空格

        # 确保 password 不为空
        if [ -n "$password" ]; then
            # 如果 local_type 不为空，使用 local_type 启动
            if [ -n "$local_type" ]; then
                # [4.x web栈适配] 守护化+stdio重定向+日志落存储盘(/tmp 是 rootfs RAM)
                start-stop-daemon -S -x /usr/sbin/npc -b -- -server=$server -vkey=$vkey -type=tcp -password=$password -local_type=$local_type >>$EXT_PLUGIN_LOG_DIR/npc.log 2>&1

            else
                start-stop-daemon -S -x /usr/sbin/npc -b -- -server=$server -vkey=$vkey -type=tcp -password=$password -target=$target >>$EXT_PLUGIN_LOG_DIR/npc.log 2>&1

            fi
        else
            # 如果 password 为空，给出错误提示
            start-stop-daemon -S -x /usr/sbin/npc -b -- -server=$server -vkey=$vkey -type=tcp >>$EXT_PLUGIN_LOG_DIR/npc.log 2>&1
        fi
    fi
}

stop() {
    killall npc
}

disable() {
    killall npc
    rm $EXT_PLUGIN_CONFIG_DIR/npc/npc.config
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

    echo "${server}" >$EXT_PLUGIN_CONFIG_DIR/npc/npc.config
    echo "${vkey}" >>$EXT_PLUGIN_CONFIG_DIR/npc/npc.config
    echo "${password}" >>$EXT_PLUGIN_CONFIG_DIR/npc/npc.config
    echo "${target}" >>$EXT_PLUGIN_CONFIG_DIR/npc/npc.config
    echo "${local_type}" >>$EXT_PLUGIN_CONFIG_DIR/npc/npc.config

    echo "配置文件已更新："
    cat $EXT_PLUGIN_CONFIG_DIR/npc/npc.config
    if killall -q -0 npc; then
        killall npc
    fi
    start
}

show() {
    Show __json_result__
}

__show_status() {
    if killall -q -0 npc; then
        local status=1
    else
        local status=0
    fi
    json_append __json_result__ status:int
}

__show_config() {
    local server=""
    local vkey=""
    local password=""
    local target=""
    local local_type=""

    if [ -f $EXT_PLUGIN_CONFIG_DIR/npc/npc.config ]; then
        . $EXT_PLUGIN_CONFIG_DIR/npc/npc.config
        target=$(echo "$target" | sed 's/-/ /g') # 将 - 转换回空格
    fi

    json_append __json_result__ server:str
    json_append __json_result__ vkey:str
    json_append __json_result__ password:str
    json_append __json_result__ target:str
    json_append __json_result__ local_type:str
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
