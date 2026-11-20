#!/bin/bash
BASH_SOURCE=$0
INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_NAME="$(jq -r '.name' $INSTALL_DIR/html/metadata.json)"
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件。配置一律落存储卷, 不落 sda3。
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

chmod +x $INSTALL_DIR/script/* 2>/dev/null
chmod +x $INSTALL_DIR/bin/netfenced 2>/dev/null

# [bin-preflight] 框架解压 ipk 时把 tar 错误静默了(2>/dev/null 且不查退出码):
# 磁盘不足时小文件成功、末尾大二进制失败, 安装却显示成功。这里显式自检。
__bin_preflight() {
    local bin="$INSTALL_DIR/bin/netfenced"
    if [ ! -f "$bin" ] || [ ! -x "$bin" ]; then
        echo "NetFence 安装自检失败: 引擎二进制缺失或不可执行 ($bin)" >&2
        df -h "$INSTALL_DIR" >&2 2>/dev/null
        return 1
    fi
    local sz
    sz=$(stat -c%s "$bin" 2>/dev/null)
    if [ -z "$sz" ] || [ "$sz" -lt 100000 ]; then
        echo "NetFence 安装自检失败: 引擎二进制不完整 (${sz:-0} 字节), 常见原因是磁盘空间不足" >&2
        df -h "$INSTALL_DIR" >&2 2>/dev/null
        return 1
    fi
    return 0
}

install()
{
    __bin_preflight || return 1

    rm -rf /usr/ikuai/www/plugins/$PLUGIN_NAME
    ln -sf $INSTALL_DIR/html /usr/ikuai/www/plugins/$PLUGIN_NAME

    # 框架据此派生 func_name=plugin_netfence, 与 metadata.name / PLUGIN_NAME / 目录名一致
    ln -sf $INSTALL_DIR/script/service.sh /usr/ikuai/function/plugin_$PLUGIN_NAME

    mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
    mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/lists
    mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/threat
    ln -sf ./install.sh $INSTALL_DIR/uninstall.sh

    # 首次安装写入「只审计不拦截」的默认策略, 已存在不覆盖(尊重用户配置)
    if [ ! -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/policy.json ]; then
        cp -f $INSTALL_DIR/script/default-policy.json $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/policy.json 2>/dev/null
    fi
    if [ ! -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/app-signatures.json ]; then
        cp -f $INSTALL_DIR/script/default-app-signatures.json $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/app-signatures.json 2>/dev/null
    fi
}

__uninstall()
{
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

boot()
{
    # 开机自启: 标志由页面 set_auto_start 写入, 只在用户开启时拉起
    if [ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ]; then
        $INSTALL_DIR/script/service.sh start >/dev/null 2>&1
    fi
}

procname=$(basename $BASH_SOURCE)
case "$procname" in
    install.sh)   install "${1-boot}" ;;
    uninstall.sh) uninstall ;;
esac
