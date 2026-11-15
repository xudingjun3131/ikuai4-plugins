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

# [规则清理] 删掉本插件加过的所有 INPUT 规则(含旧版无 -d 的一刀切 DROP), 幂等可重入
__clean_adguard_rules()
{
	local __iface=$1 __cidr __wip
	for __cidr in 10.0.0.0/8 192.168.0.0/16 172.16.0.0/12 100.64.0.0/10; do
		while iptables -C INPUT -i "$__iface" -p tcp --dport 3000 -s $__cidr -j ACCEPT 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 3000 -s $__cidr -j ACCEPT 2>/dev/null; done
		while iptables -C INPUT -i "$__iface" -p tcp --dport 53 -s $__cidr -j ACCEPT 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 53 -s $__cidr -j ACCEPT 2>/dev/null; done
	done
	for __wip in $(ip -4 addr show dev $__iface 2>/dev/null | grep inet | awk '{print($2)}' | cut -d/ -f1); do
		while iptables -C INPUT -i "$__iface" -p tcp --dport 3000 -d $__wip -j DROP 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 3000 -d $__wip -j DROP 2>/dev/null; done
		while iptables -C INPUT -i "$__iface" -p tcp --dport 53 -d $__wip -j DROP 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 53 -d $__wip -j DROP 2>/dev/null; done
	done
	# 旧版(≤2.0.6)遗留: 不带 -d 的一刀切 DROP
	while iptables -C INPUT -i "$__iface" -p tcp --dport 3000 -j DROP 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 3000 -j DROP 2>/dev/null; done
	while iptables -C INPUT -i "$__iface" -p tcp --dport 53 -j DROP 2>/dev/null; do iptables -D INPUT -i "$__iface" -p tcp --dport 53 -j DROP 2>/dev/null; done
	return 0
}


start() {

	PID=$(pidof AdGuardHome 2>/dev/null | awk '{print $NF}')
	[ -n "$PID" ] && return 0

	BIN_DIR=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin
	AGDIR=$WORK_DIR/AdGuardHome

	# 解压到存储分区(二进制不存在才解; stop 会删二进制, 升级/重开自动生效)
	mkdir -p $AGDIR/data
	if [ ! -f $AGDIR/AdGuardHome ]; then
		tar -zxf $BIN_DIR/AdGuardHome.tar.gz -C $WORK_DIR 2>/dev/null || {
			echo "解压 AdGuardHome 失败: $BIN_DIR/AdGuardHome.tar.gz(存储空间不足或包缺失)"
			return 1
		}
	fi
	if [ ! -x $AGDIR/AdGuardHome ]; then
		chmod +x $AGDIR/AdGuardHome 2>/dev/null || { echo "二进制不可执行: $AGDIR/AdGuardHome"; return 1; }
	fi
	ln -sf $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml $AGDIR/AdGuardHome.yaml
	ln -sf $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/filters $AGDIR/data/filters

	# [4.x web栈适配] 守护化启动: start-stop-daemon -b(新会话, 脱离 func 进程组/管道),
	# 失败回退子 shell 模式; stdio 全部重定向(裸 & 会继承 $(...) 管道致 SIGPIPE 打死)
	local webPort=$(grep -A5 '^http' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml 2>/dev/null | grep 'address' | awk -F ':' '{print($3)}')
	[ -z "$webPort" ] && webPort=3000
	# [日志落地] AdGuardHome 的启动报错(stdout/stderr)落文件, 不再丢 /dev/null,
	# 否则"DNS起了Web没起"这类故障完全看不见原因
	local LOG=$WORK_DIR/adguard.log
	if command -v start-stop-daemon >/dev/null 2>&1; then
		start-stop-daemon -S -x $AGDIR/AdGuardHome -b -- \
			-c $AGDIR/AdGuardHome.yaml -w $AGDIR/data --no-check-update \
			>>$LOG 2>&1
	else
		( $AGDIR/AdGuardHome -c $AGDIR/AdGuardHome.yaml -w $AGDIR/data --no-check-update >>$LOG 2>&1 & )
	fi

	# 等待监听(最多5秒), 起不来要把原因报出来(不再静默"成功")
	local i=0
	while [ $i -lt 10 ]; do
		pidof AdGuardHome >/dev/null 2>&1 && break
		sleep 0.5
		i=$((i+1))
	done
	if ! pidof AdGuardHome >/dev/null 2>&1; then
		echo "AdGuardHome 进程未存活(工作目录: $AGDIR, 端口: $webPort); 日志: $(tail -3 $LOG 2>/dev/null | tr '\n' ' ')"
		return 1
	fi
	# Web 端口探测(最多再等3秒): 进程活着但 3000 没听=HTTP 绑定失败, 必须报出来
	local __w=0
	while [ $__w -lt 6 ]; do
		curl -s -o /dev/null -m 2 http://127.0.0.1:$webPort/ 2>/dev/null && break
		sleep 0.5
		__w=$((__w+1))
	done
	if ! curl -s -o /dev/null -m 2 http://127.0.0.1:$webPort/ 2>/dev/null; then
		echo "AdGuardHome 进程在跑但 Web 端口 $webPort 未监听(DNS 端口 $(grep -A5 '^dns' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml 2>/dev/null | grep 'port:' | awk '{print($2)}') 正常则说明仅 HTTP 绑定失败); 日志尾部: $(tail -5 $LOG 2>/dev/null | tr '\n' ' | ')"
		return 1
	fi

	if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/dnshijacking" ]; then
		__do_dns_hijacking start
	fi

		# [访问控制架构] 公网直连挡 / NAT 映射放 / LAN 全开 / 私有来源放行:
		#   ① 私有段(10/8,192.168/16,172.16/12,100.64/10)来源 → ACCEPT(管理网从 wan 口接入)
		#   ② 只挡"公网直连本机 WAN IP"(-d $wip): NAT 端口映射经 DNAT 后目标被改写为
		#      LAN IP, 不打 -d wan_ip, 自然放行; LAN 口(lanX)不进本规则, 全开
		#   ③ 先清旧规则再装(版本过渡: 旧版 DROP 不带 -d, 会把 NAT 映射一起挡掉)
		for iface in $(ip link show 2>/dev/null | awk -F: '/: wan[0-9]+:/{print $2}' | tr -d ' '); do
			__clean_adguard_rules "$iface"
			for cidr in 10.0.0.0/8 192.168.0.0/16 172.16.0.0/12 100.64.0.0/10; do
				iptables -C INPUT -i "$iface" -p tcp --dport 3000 -s $cidr -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -i "$iface" -p tcp --dport 3000 -s $cidr -j ACCEPT
				iptables -C INPUT -i "$iface" -p tcp --dport 53 -s $cidr -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -i "$iface" -p tcp --dport 53 -s $cidr -j ACCEPT
			done
			for wip in $(ip -4 addr show dev $iface 2>/dev/null | grep inet | awk '{print($2)}' | cut -d/ -f1); do
				iptables -C INPUT -i "$iface" -p tcp --dport 3000 -d $wip -j DROP 2>/dev/null || iptables -A INPUT -i "$iface" -p tcp --dport 3000 -d $wip -j DROP
				iptables -C INPUT -i "$iface" -p tcp --dport 53 -d $wip -j DROP 2>/dev/null || iptables -A INPUT -i "$iface" -p tcp --dport 53 -d $wip -j DROP
			done
		done

	echo `date +%s` > $WORK_DIR/adguards_start_time
	echo "AdGuardHome 已启动(Web 管理端口: $webPort)"
	return 0
}

# [diag] 一键诊断: 页面/终端调用, 输出每一步的状态(定位"端口没起来")
# [clean_logs] 清理 AdGuardHome 查询日志/统计(配置分区实测43MB已96%满)。
# 进程在跑时不能删(删了 inode 不释放且进程持旧 fd), 只能截断;
# 过滤规则/配置/二进制不动。同时回报完整文件清单(定位占用大户)。
clean_logs()
{
	local freed=0 f sz list
	# 完整清单(KB, 数值排序; busybox sort 不支持 -rh)
	list=$(du -ak $WORK_DIR 2>/dev/null | sort -rn | head -15 | tr '\n' ';' | tr -d '"\\')
	for f in $WORK_DIR/data/querylog.json $WORK_DIR/data/querylog.json.* $WORK_DIR/data/stats.db; do
		[ -f "$f" ] || continue
		sz=$(wc -c < "$f" 2>/dev/null || echo 0)
		: > "$f" 2>/dev/null
		freed=$((freed + sz))
	done
	__json_result__="{\"mark\":\"clean_ran\",\"freed_kb\":$((freed / 1024)),\"files\":\"$list\",\"workdir\":\"$WORK_DIR\"}"
	return 0
}

diag()
{
	# [JSON 输出] 框架只把可解析为 JSON 的 stdout 放进 results, 纯文本会被丢弃,
	# 所以诊断信息必须包成 JSON 才能回传到页面/调用方
	local PID=$(pidof AdGuardHome 2>/dev/null | tr '\n' ',' )
	local BIN_OK=no; [ -x $WORK_DIR/AdGuardHome/AdGuardHome ] && BIN_OK=yes
	local PKG_OK=no; [ -f $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bin/AdGuardHome.tar.gz ] && PKG_OK=yes
	local WR_OK=no; [ -w $WORK_DIR ] && WR_OK=yes
	local SS_CMD=$(command -v start-stop-daemon 2>/dev/null || echo "不在PATH")
	local PORTS=$(ss -tln 2>/dev/null | grep -E ':(3000|6060|5353)' | tr '\n' ';' )
	local __wc=$(grep -A5 '^http' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml 2>/dev/null | grep 'address' | awk -F ':' '{print($3)}')
	[ -z "$__wc" ] && __wc=3000
	local WEB=no; curl -s -o /dev/null -m 2 http://127.0.0.1:$__wc/ 2>/dev/null && WEB=yes
	local LOGTAIL=$(tail -15 $WORK_DIR/adguard.log 2>/dev/null | tr '\n' '|' | tr '"' "'")
	local HTTPADDR=$(grep -A5 '^http' $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/AdGuardHome.yaml 2>/dev/null | grep 'address' | tr -d ' ' | cut -d: -f2- )
	echo "{\"diag\":{\"pid\":\"$PID\",\"binary\":\"$BIN_OK\",\"package\":\"$PKG_OK\",\"workdir_writable\":\"$WR_OK\",\"start_stop_daemon\":\"$SS_CMD\",\"ports_listening\":\"$PORTS\",\"web_port\":\"$__wc\",\"web_reachable\":\"$WEB\",\"http_address\":\"$HTTPADDR\",\"log_tail\":\"$LOGTAIL\"}}"
	return 0
}


stop() {

	# 杀掉全部 AdGuardHome 进程(pidof 可能返回多个: 旧版裸& 时代的孤儿会占着
	# 3000 不放, 只杀最后一个会导致新进程 HTTP 绑定失败)
	for PID in $(pidof AdGuardHome 2>/dev/null); do
		kill $PID 2>/dev/null
	done
	sleep 1
	for PID in $(pidof AdGuardHome 2>/dev/null); do
		kill -9 $PID 2>/dev/null
	done
	# 删二进制(下次 start 重新解压, 升级即生效); 保留 data(统计/会话)
	rm -f $WORK_DIR/AdGuardHome/AdGuardHome

	__do_dns_hijacking stop

		# 恢复外网对3000端口的访问(全部规则清掉, 防泄漏)
		for iface in $(ip link show | awk -F: '/: wan[0-9]+:/{print $2}' | tr -d ' '); do
			__clean_adguard_rules "$iface"
		done

	return 0
}
