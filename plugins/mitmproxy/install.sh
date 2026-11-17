#!/bin/bash
BASH_SOURCE=$0
INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_NAME="$(jq -r '.name' $INSTALL_DIR/html/metadata.json)"
chmod +x $INSTALL_DIR/script/* $INSTALL_DIR/bins/runtime/run-mitmweb.sh 2>/dev/null
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null
# [bin-preflight] 框架解压 ipk 时把 tar 错误静默了(2>/dev/null 且不查退出码),
# 磁盘不足时小文件解压成功、末尾的大文件失败, 安装却显示成功 —— 后果是插件页能打开
# 但一启动就 "No such file or directory"。本插件 bundle 有 100MB, 更容易踩中, 装完立即自检。
if [ ! -s "$INSTALL_DIR/bins/runtime/run-mitmweb.sh" ] || [ ! -s "$INSTALL_DIR/bins/runtime/usr/bin/mitmweb" ]; then
	echo "mitmproxy 安装异常: 运行时 bundle 不完整($INSTALL_DIR/bins/runtime)"
	echo "  已解压出来的文件: $(ls -la $INSTALL_DIR/bins/runtime/ 2>/dev/null | tr '\n' ' ')"
	echo "  最可能原因是插件存储分区空间不足(bundle 约 100MB), 请清理后重新安装"
	df -h 2>/dev/null | head -20
fi
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
	debug "mitmproxy 插件就绪(类型: ${type:-boot})"
	# 开机自启: 用户在页面开过"开机自启"才拉引擎
	# [exit-code-fix] 不能以 `[ -f ... ] && ... start` 收尾: 新装时 autostart 文件
	# 不存在 → `[ -f ]` 返回 1 → && 短路 → install() 返回 1 → 框架判定"安装失败",
	# 但文件和软链接早已建好, 重进页面又显示成功(用户实踩的"提示失败又成功")。
	if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ]; then
		/usr/ikuai/function/plugin_$PLUGIN_NAME start || true
	fi
	return 0
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
