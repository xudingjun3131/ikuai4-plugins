#!/bin/bash /etc/ikcommon
PLUGIN_NAME="adguard"
. /etc/mnt/plugins/configs/config.sh

# [rootfs空间修复] 工作目录放存储分区: /tmp 在 rootfs RAM 盘(仅剩~6MB),
# AdGuardHome 解压后 ~20MB, 解压到 /tmp 必 ENOSPC → 二进制起不来 → 3000 端口从不监听
WORK_DIR=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/work

set_auto_start() {
	if [ "$autostart" = "true" ]; then
		[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] || touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	fi
	return 0
}

set_dns_hijacking() {

	if [ "$dnshijacking" = "true" ]; then
		PID=$(pidof CrashCore | awk '{print $NF}')
		if [ -n "$PID" ]; then
			if iptables -t nat -L shellcrash_dns -n 2>/dev/null | grep -q '^Chain'; then
				echo "检测到Crash正在劫持本机DNS，请先停止Crash或禁用其DNS劫持。"
				return 1
			fi
		fi

		[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking" ] || touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking
		__do_dns_hijacking start
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking
		__do_dns_hijacking stop
	fi
	return 0
}

__do_dns_hijacking() {
	action=$1

	if [ "$action" = "start" ]; then
		iptables -t nat -A PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 5353
		iptables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 5353
		ip6tables -t nat -A PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 5353
		ip6tables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 5353
	fi
	if [ "$action" = "stop" ]; then
		while true; do
			dns5353=$(iptables -t nat -vnL PREROUTING --line-number | grep "5353" | wc -l)
			if [ $dns5353 -gt 0 ]; then
				iptables -t nat -D PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 5353
				iptables -t nat -D PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 5353
				ip6tables -t nat -D PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 5353
				ip6tables -t nat -D PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 5353
			else
				break
			fi
		done
	fi

}

show() {
	Show __json_result__
}

__show_data() {

	local status=0

	PID=$(pidof AdGuardHome | awk '{print $NF}')
	[ -n "$PID" ] && status=1
	[ -f /tmp/iktmp/plugins/adguard_installed ] || status=2

	local autostart=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1

	local dnshijacking=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking ] && dnshijacking=1

	local listenPort="$(grep -A 5 '^dns' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml | grep 'port:' | awk '{print($2)}')"

	local webPort="$(grep -A 5 '^http' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml | grep 'address' | awk -F ':' '{print($3)}')"


	local runningTime=""
	start_time=$(cat $WORK_DIR/adguards_start_time 2>/dev/null)
	if [ -n "$start_time" ]; then 
		time=$((`date +%s`-start_time))
		day=$((time/86400))
		[ "$day" = "0" ] && day='' || day="$day天"
		time=`date -u -d @${time} +%H小时%M分%S秒`
		runningTime="已运行: ${day}${time}"
	fi

	json_append __json_result__ autostart:int
	json_append __json_result__ status:int
	json_append __json_result__ listenPort:int
	json_append __json_result__ webPort:int
	json_append __json_result__ dnshijacking:int
	json_append __json_result__ runningTime:str

}

start() {

	PID=$(pidof AdGuardHome | awk '{print $NF}')
	[ -n "$PID" ] && return 0

	BIN_DIR=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin
	AGDIR=$WORK_DIR/AdGuardHome

	# 解压到存储分区(二进制不存在才解; stop 会删二进制, 升级/重开自动生效)
	mkdir -p $AGDIR/data
	if [ ! -f $AGDIR/AdGuardHome ]; then
		tar -zxf $BIN_DIR/AdGuardHome.tar.gz -C $WORK_DIR || { echo "解压 AdGuardHome 失败, 请检查存储空间"; return 1; }
	fi
	ln -sf $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml $AGDIR/AdGuardHome.yaml
	ln -sf $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/filters $AGDIR/data/filters
	chmod +x $AGDIR/AdGuardHome

	# [4.x web栈适配] start-stop-daemon -b 守护化+stdio重定向:
	# 裸 & 会让守护进程继承 func 的 $(...) 管道 → 阻塞60s超时 → SIGPIPE 打死 → 端口从不监听
	start-stop-daemon -S -x $AGDIR/AdGuardHome -b -- \
		-c $AGDIR/AdGuardHome.yaml -w $AGDIR/data --no-check-update \
		>/dev/null 2>&1

	if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking" ]; then
		__do_dns_hijacking start
	fi

	# 禁止外网访问(幂等: -C 探测后追加, 防重复 start 堆规则)
	for iface in $(ip link show | awk -F: '/: wan[0-9]+:/{print $2}' | tr -d ' '); do
		iptables -C INPUT -i "$iface" -p tcp --dport 3000 -j DROP 2>/dev/null || iptables -A INPUT -i "$iface" -p tcp --dport 3000 -j DROP
		iptables -C INPUT -i "$iface" -p tcp --dport 53 -j DROP 2>/dev/null || iptables -A INPUT -i "$iface" -p tcp --dport 53 -j DROP
	done

	echo `date +%s` > $WORK_DIR/adguards_start_time
	return 0
}


stop() {

	PID=$(pidof AdGuardHome | awk '{print $NF}')
	[ -n "$PID" ] && kill $PID
	# 删二进制(下次 start 重新解压, 升级即生效); 保留 data(统计/会话)
	rm -f $WORK_DIR/AdGuardHome/AdGuardHome

	__do_dns_hijacking stop

	# 恢复外网对3000端口的访问
	for iface in $(ip link show | awk -F: '/: wan[0-9]+:/{print $2}' | tr -d ' '); do
		iptables -D INPUT -i "$iface" -p tcp --dport 3000 -j DROP
		iptables -D INPUT -i "$iface" -p tcp --dport 53 -j DROP
	done

	return 0
}
