#!/bin/bash /etc/ikcommon
PLUGIN_NAME="uu"
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

start() {

	pidof uuplugin >/dev/null 2>&1 && return 0

	if [ -f /usr/sbin/uu/uuplugin_monitor.sh ]; then
		# [4.x web栈适配] stdio 重定向(stderr 泄漏会阻塞 $(...) 致 SIGPIPE)
		( /bin/sh /usr/sbin/uu/uuplugin_monitor.sh >/dev/null 2>&1 & )
	else
		( $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/script/uu_install.sh openwrt $(uname -m) >/dev/null 2>&1 & )
	fi
	sleep 1
	return 0
}

stop() {
	for pid in $(ps | grep "uuplugin_monitor" | grep -v "grep" | awk '{print $1}'); do
		kill -9 $pid
	done

	for pid in $(ps | grep "uuplugin" | grep -v "grep" | awk '{print $1}'); do
		kill -9 $pid
	done
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
	local autostart=0
	local status=0

    [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] && autostart=1
    [ -f /tmp/iktmp/plugins/${PLUGIN_NAME}_installed ] || status=2
    pidof uuplugin >/dev/null 2>&1 && status=1
    
    json_append __json_result__ autostart:int
	json_append __json_result__ status:int
}
