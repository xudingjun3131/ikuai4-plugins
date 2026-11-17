#!/bin/bash /etc/ikcommon 
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null
script_path=$(readlink -f "${BASH_SOURCE[0]}")
plugin_dir=$(dirname "$script_path")

start(){
	killall vlmcsd
	if [ ! -f /sbin/vlmcsd ];then
	chmod +x $plugin_dir/../bin/vlmcsd
	ln -s $plugin_dir/../bin/vlmcsd /sbin/vlmcsd
	fi
	if [ ! -f $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini ];then
		cp $plugin_dir/../bin/vlmcsd.ini $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini
	fi

	if [ ! -f /usr/ikuai/www/vlmcsd.txt ];then
		ln -s $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini /usr/ikuai/www/vlmcsd.txt
	fi

	vlmcsd_number=`iptables -vnL INPUT --line-number|grep "1688"|wc -l`
	if [ $vlmcsd_number -eq 0 ];then
		iptables -I INPUT -p tcp --dport 1688 -j ACCEPT
	fi
	vlmcsd -i $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini -L 0.0.0.0:1688 >>/tmp/vlmcsd.log &

}


vlmcsd_start(){

	if [ ! -f $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini ];then
		cp $plugin_dir/../bin/vlmcsd.ini $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini
	fi

start
}

stop(){
killall vlmcsd
rm $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini
echo "stop"  >>/tmp/vlmcsd.logs
}


config(){

 if [ -f /tmp/iktmp/import/file ]; then
   filesize=$(stat -c%s "/tmp/iktmp/import/file")
   if [ $filesize -lt 524288 ]; then
	mv /tmp/iktmp/import/file $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini
	Vlmcsd_start
   fi
   
 fi

}

show(){
    local __filter=$(sql_auto_get_filter)
    local __order=$(sql_auto_get_order)
    local __limit=$(sql_auto_get_limit)
    local __where="$__filter $__order $__limit"
    Show __json_result__
}

__show_status(){
local status=0
if [ ! -f /usr/ikuai/www/vlmcsd.txt ];then

	ln -s $EXT_PLUGIN_CONFIG_DIR/vlmcsd/vlmcsd.ini /usr/ikuai/www/vlmcsd.txt
fi
if killall -q -0 vlmcsd ;then
	local status=1
else
	local status=0
fi
json_append __json_result__ status:int
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    *)
      ;;
esac
