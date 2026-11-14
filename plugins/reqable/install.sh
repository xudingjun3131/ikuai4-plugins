#!/bin/bash
BASH_SOURCE=$0
INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_NAME="$(jq -r '.name' $INSTALL_DIR/html/metadata.json)"
chmod +x $INSTALL_DIR/script/* $INSTALL_DIR/bins/*
. /etc/mnt/plugins/configs/config.sh
install()
{
	# 安装类型: new 新装 / upgrade 保留配置更新 / reinstall 不保留配置更新 / boot 开机
	# (框架每次开机对每个已装插件跑 install.sh boot, 见 init_plugins.sh boot())
	type=$1
	rm -rf /usr/ikuai/www/plugins/$PLUGIN_NAME
	ln -sf $INSTALL_DIR/html /usr/ikuai/www/plugins/$PLUGIN_NAME
	ln -sf $INSTALL_DIR/script/service.sh /usr/ikuai/function/plugin_$PLUGIN_NAME
	ln -sf ./install.sh $INSTALL_DIR/uninstall.sh
	if [ "$type" = "new" -o "$type" = "reinstall" ]; then
		mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
	fi
	debug "reqable 插件就绪(类型: ${type:-boot})"
	# 开机自启: 用户在页面开过"开机自启"才拉引擎(与 lucky 等同构)
	[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] && /usr/ikuai/function/plugin_$PLUGIN_NAME start
}
__uninstall()
{
	/usr/ikuai/function/plugin_$PLUGIN_NAME stop >/dev/null 2>&1
	rm -rf $INSTALL_DIR
	rm -rf /usr/ikuai/www/plugins/$PLUGIN_NAME
	rm -rf $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
	rm -f $EXT_PLUGIN_IPK_DIR/$PLUGIN_NAME.ipk
	rm -f $EXT_PLUGIN_LOG_DIR/$PLUGIN_NAME.log
	rm -f /usr/ikuai/function/plugin_$PLUGIN_NAME
}
uninstall()
{
	__uninstall >/dev/null 2>&1
}
procname=$(basename $BASH_SOURCE)
if [ "$procname" = "install.sh" ];then
        install ${1-boot}
elif [ "$procname" = "uninstall.sh" ];then
        uninstall
fi
