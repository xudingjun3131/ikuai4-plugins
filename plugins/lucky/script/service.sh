#!/bin/bash /etc/ikcommon
PLUGIN_NAME="lucky" 
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

start() {

    PID=$(pidof lucky | awk '{print $NF}')
	[ -n "$PID" ] && return

    chrootmgt install_disk
    chrootmgt mount_plugin $PLUGIN_NAME

    chrootmgt run $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/lucky -cd $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME >$EXT_PLUGIN_LOG_DIR/lucky.log 2>&1 &

    mkdir -p /tmp/lucky
    echo `date +%s` > /tmp/lucky/lucky_start_time
    return 0
}

stop() {
    killall lucky

    chrootmgt umount_plugin $PLUGIN_NAME
    rm -f /tmp/lucky/lucky_start_time
}

set_auto_start() {
	if [ "$autostart" = "true" ];then
		[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] || touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	fi
	return 0
}

show() {

    Show __json_result__
}

__show_data() {

    local status=0

	PID=$(pidof lucky | awk '{print $NF}')
	[ -n "$PID" ] && status=1
	[ -f /tmp/iktmp/plugins/lucky_installed ] || status=2

    local autostart=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1

    local runningTime=""
	start_time=$(cat /tmp/lucky/lucky_start_time)
	if [ -n "$start_time" ]; then 
		time=$((`date +%s`-start_time))
		day=$((time/86400))
		[ "$day" = "0" ] && day='' || day="$day天"
		time=`date -u -d @${time} +%H小时%M分%S秒`
		runningTime="已运行: ${day}${time}"
	fi

    local webPort="16601"  # 默认端口，TODO： 目前尚无法准确获取真实端口号

    json_append __json_result__ autostart:int
	json_append __json_result__ status:int
	json_append __json_result__ runningTime:str
    json_append __json_result__ webPort:str
}
