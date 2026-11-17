#!/bin/bash /etc/ikcommon
PLUGIN_NAME="vnts"
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

start()
{
	if [ ! -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config" ]; then
		echo "启动VNTS失败,请先保存配置！" 
		return 1
	else
		. $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config
	fi

	[ -d $EXT_PLUGIN_LOG_DIR ] || mkdir -p $EXT_PLUGIN_LOG_DIR
	echo "启动VNTS" > $EXT_PLUGIN_LOG_DIR/vnts.log

	# [vnts-2.x] 上游 v2.0.9 起 CLI 改为 TOML 配置文件(旧 -p/-P/-U/-W/-g/-m
	# 旗标已全部移除), web UI 内嵌进二进制(不再需要 static/ 目录)。
	# 按页面设置生成 config.toml; 证书/租约db落数据分区(配置分区放不下)。
	local CONF=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config.toml
	local DATA_DIR=$EXT_PLUGIN_INSTALL_DIR/../.vnts_data
	mkdir -p $DATA_DIR 2>/dev/null
	local NET="${gateway%.*}.0/${mask:-24}"
	cat > $CONF <<-EOT
tcp_bind = "[::]:${serviceport:-29889}"
quic_bind = "[::]:${serviceport:-29889}"
network = "$NET"
white_list = []
lease_duration = 86400
web_bind = "[::]:${webport:-29888}"
username = "${username:-ikuai}"
password = "${password}"
persistence = true
EOT

	killall vnts
	# [坑48] 必须直接 cd+后台, 不能用 (cd .. && cmd) & —— 子 shell 会持有
	# 调用方管道写端, 命令替换等不到 EOF 直接阻塞 60s
	cd $DATA_DIR
	$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/vnts --conf $CONF >> $EXT_PLUGIN_LOG_DIR/vnts.log 2>&1 &
	sleep 1

	if killall -q -0 vnts;then
		echo "启动VNTS成功"
		return 0
	fi
	
	echo "启动VNTS失败,请检查配置文件" 
	return 1
}

stop()
{
	if killall -q -0 vnts;then
		killall vnts
	fi
}

save()
{
	local configpath=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config

	[ "$serviceport" ] || serviceport=29889
	[ "$webport" ] || webport=29888
	[ "$username" ] || username="admin"
	[ "$password" ] || password="admin"
	[ "$gateway" ] || gateway="10.26.0.1"
	[ "$mask" ] || mask="255.255.255.0"

	echo "serviceport=${serviceport}" > $configpath
	echo "webport=${webport}" >> $configpath
	echo "username=${username}" >> $configpath
	echo "password=${password}" >> $configpath
	echo "gateway=${gateway}" >> $configpath
	echo "mask=${mask}" >> $configpath
	
	return 0
}

set_auto_start() {
	if [ "$autostart" = "true" ];then
		[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] || touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	fi
	return 0
}

show(){
    Show __json_result__
}

__show_data(){

	local autostart=0
	local status=0
	local serviceport=29889
	local webport=29888
	local username="admin"
	local password="admin"
	local gateway="10.26.0.1"
	local mask="255.255.255.0"

	if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config" ]; then
		. $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/config
	fi

	[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] && autostart=1

	[ -f /tmp/iktmp/plugins/vnts_installed ] || status=2

	if killall -q -0 vnts;then
		status=1
	fi

	json_append __json_result__ autostart:int
	json_append __json_result__ status:int
	json_append __json_result__ serviceport:int
	json_append __json_result__ webport:int
	json_append __json_result__ username:str
	json_append __json_result__ password:str
	json_append __json_result__ gateway:str
	json_append __json_result__ mask:str
}
