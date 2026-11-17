#!/bin/bash
BASH_SOURCE=$0
INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_NAME="$(jq -r '.name' $INSTALL_DIR/html/metadata.json)"
chmod +x $INSTALL_DIR/script/*
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

install()
{
  # 安装类型如下：
	# 1、new:新安装; 
	# 2、upgrade:保留配置更新; 
	# 3、reinstall:不保留配置更新; 
	# 4、boot:开机启动
  type=$1 
  
	rm -f /tmp/iktmp/plugins/${PLUGIN_NAME}_installed

	rm -rf /usr/ikuai/www/plugins/$PLUGIN_NAME
	ln -sf $INSTALL_DIR/html /usr/ikuai/www/plugins/$PLUGIN_NAME
	ln -sf $INSTALL_DIR/script/service.sh /usr/ikuai/function/plugin_$PLUGIN_NAME
	ln -sf ./install.sh $INSTALL_DIR/uninstall.sh
	mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME

  if [ ! -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/extapi.token ]; then
    /usr/ikuai/function/plugin_$PLUGIN_NAME renewToken
  fi

  if [ ! -f /tmp/iktmp/extapi.token ]; then
    ln -fs $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/extapi.token /tmp/iktmp/extapi.token
  fi

  if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/extapi_enabled" ]; then
		/usr/ikuai/function/plugin_$PLUGIN_NAME start
	fi  

	touch /tmp/iktmp/plugins/${PLUGIN_NAME}_installed
}

__uninstall()
{
	/usr/ikuai/function/plugin_$PLUGIN_NAME stop
	rm -f /tmp/iktmp/plugins/${PLUGIN_NAME}_installed

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
