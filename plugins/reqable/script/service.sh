#!/bin/bash /etc/ikcommon
# 小黄鸟抓包插件 service.sh
# 三层架构:
#   层1 iptables(本脚本): 按设备(MAC/IP)+接口/VLAN+端口+时段 精确引流, 绝不全局
#   层2 引擎 scope.json:  按域名+目标IP+GEOIP 决定是否 MITM 解密
#   层3 引擎记录层:       按路径/方法/UA/JA3/内容类型/大小 决定是否保存
PLUGIN_NAME=reqable
. /etc/mnt/plugins/configs/config.sh

BIN=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/reqable-x86_64
DATADIR=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
PROXY_PORT=18080       # 显式代理(设备手动设代理 router:18080)
TRANSPARENT_PORT=18082 # 透明监听(iptables REDIRECT 的目标)
API_PORT=18081         # Web 界面/API
PIDFILE=$DATADIR/reqable.pid
LOGDIR=$EXT_PLUGIN_LOG_DIR/$PLUGIN_NAME
CAPTURE_FLAG=$DATADIR/capture_on     # 抓包总开关
RULES_FLAG=$DATADIR/rules_on         # iptables 规则当前是否生效(调度器维护)
DEVICES_FILE=$DATADIR/devices.json   # 选中的设备 [{ip,mac,name}]
SCOPE_FILE=$DATADIR/scope.json       # 引擎层范围(域名/IP/GEOIP/记录层)
SCHED_FILE=$DATADIR/schedule.json    # 时段 {enabled,start,stop,days:[1-7]}
GROUPS_DIR=$DATADIR/groups           # 设备分组(命名快捷选择)
SCHED_PIDFILE=$DATADIR/scheduler.pid # 时段调度循环
# [geo-url-fix] db-ip 旧地址已404(目录结构变更), 换 jsdelivr 镜像的 geolite2-country
# (同格式 起IP,止IP,国家码, 明文CSV免解压); 失败再试 dbip-country 镜像
# [geo-url-fix2] 设备访问不了 cdn.jsdelivr.net(实测两源均不可达, 但同设备能正常
# 下载本仓库 Pages 上的 ipk)。主源改为随本仓库 Pages 发布的 GEOIP 数据(CI 构建时
# 从 jsdelivr 抓取并部署), 与 ipk 同 CDN 必然可达; jsdelivr 保留作兜底。
GEOIP_URL="https://xudingjun3131.github.io/ikuai4-plugins/geoip/geolite2-country-ipv4.csv"
GEOIP_URL2="https://cdn.jsdelivr.net/gh/sapics/ip-location-db@main/geolite2-country/geolite2-country-ipv4.csv"

# 确保目录存在(配置动作可能先于 start 被调用)
mkdir -p $DATADIR $LOGDIR $GROUPS_DIR 2>/dev/null

# ============ 层1: iptables 规则生成(按维度精确匹配, 幂等) ============

# 从 devices.json 读设备列表, 生成 nat REDIRECT 规则(每设备一条)
# 匹配优先级: MAC 优先(DHCP 换 IP 不失效), 无 MAC 用 -s IP
iptables_add()
{
	local lanip=$(ip route 2>/dev/null | awk '/src/ {for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
	[ -z "$lanip" ] && lanip=$(ifconfig br-lan 2>/dev/null | awk '/inet /{print $2}' | cut -d: -f2)

	# 端口(默认 80,443)
	local ports="80,443"
	[ -f $DATADIR/ports ] && ports=$(cat $DATADIR/ports)
	# 接口(空=全部)
	local ifaces=""
	[ -f $DATADIR/interfaces ] && ifaces=$(cat $DATADIR/interfaces)
	# 设备列表
	local devs="[]"
	[ -f $DEVICES_FILE ] && devs=$(cat $DEVICES_FILE)

	# 链(幂等)
	iptables -t nat -N REQABLE_REDIRECT 2>/dev/null
	iptables -t nat -C PREROUTING -j REQABLE_REDIRECT 2>/dev/null || iptables -t nat -I PREROUTING -j REQABLE_REDIRECT
	iptables -t nat -F REQABLE_REDIRECT
	iptables -N REQABLE_QUIC 2>/dev/null
	iptables -C FORWARD -j REQABLE_QUIC 2>/dev/null || iptables -I FORWARD -j REQABLE_QUIC
	iptables -F REQABLE_QUIC

	# 排除路由器自身(否则 LAN 访问路由器管理页也会被截)
	[ -n "$lanip" ] && iptables -t nat -A REQABLE_REDIRECT -d $lanip -j RETURN

	# 接口匹配前缀
	local ifm=""
	if [ -n "$ifaces" ]; then
		local i
		for i in $(echo $ifaces | tr ',' ' '); do ifm="$ifm -i $i"; done
	fi

	# 逐设备生成规则
	local count=0
	while read -r d; do
		[ -z "$d" ] && continue
		local mac=$(echo "$d" | jq -r '.mac // empty' 2>/dev/null)
		local ip=$(echo "$d" | jq -r '.ip // empty' 2>/dev/null)
		local match=""
		[ -n "$mac" ] && [ "$mac" != "null" ] && match="-m mac --mac-source $mac"
		[ -z "$match" ] && [ -n "$ip" ] && [ "$ip" != "null" ] && match="-s $ip"
		[ -z "$match" ] && continue
		iptables -t nat -A REQABLE_REDIRECT $ifm -p tcp -m multiport --dports $ports $match -j REDIRECT --to-ports $TRANSPARENT_PORT
		# QUIC(UDP443) 阻断, 强制浏览器回退 TCP(否则 HTTP/3 绕过 MITM)
		iptables -A REQABLE_QUIC $ifm -p udp --dport 443 $match -j DROP
		count=$((count+1))
	done < <(echo "$devs" | jq -c '.[]' 2>/dev/null)
	touch $RULES_FLAG
	echo "规则已生效: $count 台设备"
	return 0
}

iptables_del()
{
	iptables -t nat -D PREROUTING -j REQABLE_REDIRECT 2>/dev/null
	iptables -t nat -F REQABLE_REDIRECT 2>/dev/null
	iptables -t nat -X REQABLE_REDIRECT 2>/dev/null
	iptables -D FORWARD -j REQABLE_QUIC 2>/dev/null
	iptables -F REQABLE_QUIC 2>/dev/null
	iptables -X REQABLE_QUIC 2>/dev/null
	rm -f $RULES_FLAG
	return 0
}

# 当前是否在时段内(无时段配置=全天)
in_schedule()
{
	[ -f $SCHED_FILE ] || return 0
	local enabled=$(jq -r '.enabled // false' $SCHED_FILE 2>/dev/null)
	[ "$enabled" != "true" ] && return 0
	local start=$(jq -r '.start // "00:00"' $SCHED_FILE)
	local stop=$(jq -r '.stop // "23:59"' $SCHED_FILE)
	local days=$(jq -r '(.days // []) | join(",")' $SCHED_FILE 2>/dev/null)
	local now=$(date +%H:%M)
	local dow=$(date +%u) # 1=周一..7=周日
	if [ -n "$days" ]; then
		echo ",$days," | grep -q ",$dow," || return 1
	fi
	# 时段比较(支持跨天: start > stop 表示跨午夜)
	if [ "$start" \< "$stop" ]; then
		if [ "$now" \> "$start" ] && [ "$now" \< "$stop" ]; then return 0; fi
		if [ "$now" = "$start" ] || [ "$now" = "$stop" ]; then return 0; fi
		return 1
	else
		if [ "$now" \> "$start" ] || [ "$now" = "$start" ]; then return 0; fi
		if [ "$now" \< "$stop" ] || [ "$now" = "$stop" ]; then return 0; fi
		return 1
	fi
}

# 调度循环: 每 30s 按 总开关+时段 协调规则(随代理进程存活)
scheduler_loop()
{
	while kill -0 $(cat $PIDFILE 2>/dev/null) 2>/dev/null; do
		if [ -f $CAPTURE_FLAG ] && in_schedule; then
			[ -f $RULES_FLAG ] || iptables_add >/dev/null 2>&1
		else
			[ -f $RULES_FLAG ] && iptables_del
		fi
		sleep 10
	done
	iptables_del
}

start()
{
	mkdir -p $DATADIR $LOGDIR $GROUPS_DIR
	if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
		return 0
	fi
	$BIN -proxy-addr 0.0.0.0:$PROXY_PORT -transparent-addr 0.0.0.0:$TRANSPARENT_PORT \
		-api-addr 0.0.0.0:$API_PORT \
		-data-dir $DATADIR -transparent -scope-file $SCOPE_FILE \
		>>$LOGDIR/reqable.log 2>&1 </dev/null &
	echo $! > $PIDFILE
	sleep 1
	# 健康检查 + CA 预热
	wget -q -O /dev/null -T3 http://127.0.0.1:$API_PORT/api/ca.pem 2>/dev/null
	# 启动自检: 进程或端口不在要报出来(否则用户看到"已启动"但端口没监听)
	if ! kill -0 $(cat $PIDFILE) 2>/dev/null; then
		echo "引擎启动失败, 日志尾部:"
		tail -3 $LOGDIR/reqable.log 2>/dev/null
		rm -f $PIDFILE
		return 1
	fi
	# 时段调度循环(总开关开着才会上规则)
	[ -f $SCHED_PIDFILE ] && kill $(cat $SCHED_PIDFILE) 2>/dev/null
	( scheduler_loop ) >/dev/null 2>&1 &
	echo $! > $SCHED_PIDFILE
	return 0
}

stop()
{
	[ -f $SCHED_PIDFILE ] && kill $(cat $SCHED_PIDFILE) 2>/dev/null
	rm -f $SCHED_PIDFILE
	[ -f $PIDFILE ] && kill $(cat $PIDFILE) 2>/dev/null
	rm -f $PIDFILE
	iptables_del
	return 0
}

# ============ 配置动作(页面调用) ============

# 总开关: set_capture capture=true|false
set_capture()
{
	if [ "$capture" = "true" ]; then
		# 引擎没跑先拉起来: 规则指向没监听的端口会把勾选设备的 80/443 全弄断
		if [ ! -f $PIDFILE ] || ! kill -0 $(cat $PIDFILE) 2>/dev/null; then
			start || { echo "引擎未运行且启动失败, 抓包无法开启"; return 1; }
		fi
		touch $CAPTURE_FLAG
		if in_schedule; then
			iptables_add
		else
			echo "抓包已开启, 但当前不在设定时段内, 规则未生效(到点自动生效)"
		fi
	else
		rm -f $CAPTURE_FLAG
		iptables_del
		echo "抓包已关闭, 网络规则已撤"
	fi
	return 0
}

# 设备选择: set_devices devices='[{"ip":"..","mac":"..","name":".."}]'
set_devices()
{
	echo "$devices" | jq -e 'type == "array"' >/dev/null 2>&1 || { echo "devices 参数必须是 JSON 数组"; return 1; }
	echo "$devices" | jq '.' > $DEVICES_FILE
	# 规则热更新(总开关开着才重建)
	if [ -f $CAPTURE_FLAG ] && in_schedule; then
		iptables_add
	fi
	local n=$(jq 'length' $DEVICES_FILE)
	echo "已更新设备列表($n 台)"
	return 0
}

# 引擎层范围: set_scope scope='{"domain_mode":...,"domains":[...],...}'
set_scope()
{
	echo "$scope" | jq -e 'type == "object"' >/dev/null 2>&1 || { echo "scope 参数必须是 JSON 对象"; return 1; }
	echo "$scope" | jq '.' > $SCOPE_FILE
	# SIGHUP 热重载(抓包不中断)
	if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
		kill -HUP $(cat $PIDFILE) 2>/dev/null
	fi
	echo "捕获范围已更新并热重载"
	return 0
}

# 端口: set_ports ports='80,443,8080'
set_ports()
{
	echo "$ports" | grep -qE '^[0-9]+(,[0-9]+)*$' || { echo "ports 格式: 80,443"; return 1; }
	echo "$ports" > $DATADIR/ports
	[ -f $CAPTURE_FLAG ] && [ -f $RULES_FLAG ] && iptables_add
	echo "端口已更新: $ports"
	return 0
}

# 接口/VLAN: set_interfaces ifaces='lan1,lan2'(空=全部)
set_interfaces()
{
	if [ -n "$ifaces" ]; then
		echo "$ifaces" | grep -qE '^[a-z0-9.,]+$' || { echo "ifaces 格式: lan1,lan2"; return 1; }
		echo "$ifaces" > $DATADIR/interfaces
	else
		rm -f $DATADIR/interfaces
	fi
	[ -f $CAPTURE_FLAG ] && [ -f $RULES_FLAG ] && iptables_add
	echo "接口已更新: ${ifaces:-全部}"
	return 0
}

# 时段: set_schedule schedule='{"enabled":true,"start":"09:00","stop":"18:00","days":[1,2,3,4,5]}'
set_schedule()
{
	echo "$schedule" | jq -e 'type == "object"' >/dev/null 2>&1 || { echo "schedule 参数必须是 JSON 对象"; return 1; }
	echo "$schedule" | jq '.' > $SCHED_FILE
	# 立即协调一次
	if [ -f $CAPTURE_FLAG ]; then
		if in_schedule; then iptables_add; else iptables_del; fi
	fi
	echo "时段已更新"
	return 0
}

# 设备分组: save_group name='手机' devices='[...]'
save_group()
{
	[ -z "$name" ] && { echo "缺少 name"; return 1; }
	echo "$devices" | jq -e 'type == "array"' >/dev/null 2>&1 || { echo "devices 必须是 JSON 数组"; return 1; }
	echo "$name" | grep -qE '^[A-Za-z0-9_-]+$' || { echo "name 只允许字母数字_-"; return 1; }
	echo "$devices" | jq '.' > $GROUPS_DIR/$name.json
	echo "分组已保存: $name"
	return 0
}

del_group()
{
	[ -f $GROUPS_DIR/$name.json ] && rm -f $GROUPS_DIR/$name.json
	echo "分组已删除: $name"
	return 0
}

# GEOIP 数据更新(下载 db-ip lite CSV)
update_geoip()
{
	echo "正在下载 GEOIP 数据..."
	# 明文CSV直落(免解压); 主源失败自动试备用源; 临时文件原子替换防半截文件
	# [wget-arg-order] busybox wget 不重排参数: 选项必须全在 URL 之前, 放到
	# URL 后面会被当成额外 URL 导致秒败(曾表现为"两个源均不可达")。
	# 超时放到 120s: 设备到 CDN 实测 ~550KB/s, 11MB 需 ~20s, 留足余量。
	local ok=0 __u
	for __u in "$GEOIP_URL" "$GEOIP_URL2"; do
		if wget -q -T 120 -O $DATADIR/geoip.csv.tmp "$__u" 2>/dev/null && [ -s $DATADIR/geoip.csv.tmp ]; then
			ok=1
			break
		fi
		rm -f $DATADIR/geoip.csv.tmp
	done
	if [ "$ok" = "1" ]; then
		mv -f $DATADIR/geoip.csv.tmp $DATADIR/geoip.csv
		# SIGHUP 让引擎重载
		[ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null && kill -HUP $(cat $PIDFILE) 2>/dev/null
		local lines=$(wc -l < $DATADIR/geoip.csv 2>/dev/null)
		echo "GEOIP 数据已更新($lines 条)"
		return 0
	fi
	rm -f $DATADIR/geoip.csv.tmp
	echo "GEOIP 下载失败, 请检查网络(两个源均不可达)"
	return 1
}

set_auto_start()
{
	if [ "$autostart" = "true" ]; then
		[ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ] || touch $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	else
		rm -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart
	fi
	return 0
}

clear_flows()
{
	wget -q -O /dev/null --method=DELETE http://127.0.0.1:$API_PORT/api/flows 2>/dev/null
	return 0
}

# ============ show ============

show()
{
	Show __json_result__
}

# [CA下载通道] 页面是HTTPS(路由器Web UI), 直链 http://IP:18081/api/ca.pem 会被
# 浏览器当混合内容拦掉(现象: 点了一闪而过没下载)。改走同源的 show&TYPE=ca:
# CA 内容经本接口(base64)回传, 页面端 Blob 下载, 全程同源无混合内容。
# [diag] GEOIP 下载失败诊断: 不吞错误, 两种参数顺序都测, 连DNS/目录可写性一起回报
__show_diag()
{
	# [diag] 磁盘实况: 两个分区的余量 + 配置目录占用TOP(定位谁吃满了空间)
	local __r="" __u __sz __err __wr __dfc __dfi __du
	__dfc=$(df -h $DATADIR 2>/dev/null | tail -1 | tr -s ' ' | tr -d '"')
	__dfi=$(df -h $EXT_PLUGIN_INSTALL_DIR 2>/dev/null | tail -1 | tr -s ' ' | tr -d '"')
	__du=$(du -sh $EXT_PLUGIN_CONFIG_DIR/* 2>/dev/null | sort -rh | head -6 | tr '\n' ';' | tr -d '"')
	__wr=$([ -w "$DATADIR" ] && echo yes || echo no)
	__json_result__="{\"mark\":\"diag_ran\",\"df_config\":\"$__dfc\",\"df_install\":\"$__dfi\",\"du_config_top\":\"$__du\",\"writable\":\"$__wr\",\"datadir\":\"$DATADIR\"}"
	return 0
}

__show_ca()
{
	local caf="" f
	for f in $DATADIR/ca.pem $DATADIR/mitmproxy-ca-cert.pem $DATADIR/ca-cert.pem; do
		[ -f "$f" ] && caf="$f" && break
	done
	if [ -z "$caf" ]; then
		__json_result__='{"ca_b64":""}'
		return 0
	fi
	local b64=$(base64 "$caf" 2>/dev/null | tr -d '\n')
	__json_result__="{\"ca_b64\":\"$b64\"}"
	return 0
}

__show_data()
{
	# json_append 按同名字段变量取值, 局部变量必须小写同名
	local status=0
	[ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null && status=1
	local capture=0
	[ -f $CAPTURE_FLAG ] && capture=1
	local rulesOn=0
	[ -f $RULES_FLAG ] && rulesOn=1
	local autostart=0
	[ -f $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart ] && autostart=1
	local caok=0
	{ [ -f $DATADIR/ca.pem ] || [ -f $DATADIR/mitmproxy-ca-cert.pem ] || [ -f $DATADIR/ca-cert.pem ]; } && caok=1
	local flows=0
	flows=$(wget -q -O - http://127.0.0.1:$API_PORT/api/flows 2>/dev/null | jq '.flows | length' 2>/dev/null)
	[ -z "$flows" ] && flows=0
	local proxyPort=$PROXY_PORT
	local apiPort=$API_PORT
	local geoLoaded=0
	[ -s $DATADIR/geoip.csv ] && geoLoaded=1
	local inWindow=0
	in_schedule && inWindow=1

	# 当前配置(JSON 原样回传)
	local devices='[]'
	[ -f $DEVICES_FILE ] && devices=$(cat $DEVICES_FILE)
	local scope='{}'
	[ -f $SCOPE_FILE ] && scope=$(cat $SCOPE_FILE)
	local schedule='{"enabled":false}'
	[ -f $SCHED_FILE ] && schedule=$(cat $SCHED_FILE)
	local ports='80,443'
	[ -f $DATADIR/ports ] && ports=$(cat $DATADIR/ports)
	local interfaces=''
	[ -f $DATADIR/interfaces ] && interfaces=$(cat $DATADIR/interfaces)
	# 分组列表
	local groups='[]'
	if [ -d $GROUPS_DIR ]; then
		groups=$(for g in $GROUPS_DIR/*.json; do [ -f "$g" ] && jq -c --arg n "$(basename $g .json)" '{name:$n, devices:.}' "$g"; done 2>/dev/null | jq -s '.')
	fi
	[ -z "$groups" ] && groups='[]'

	json_append __json_result__ status:int capture:int rulesOn:int autostart:int caok:int flows:int
	json_append __json_result__ proxyPort:int apiPort:int geoLoaded:int inWindow:int
	json_append __json_result__ devices:json scope:json schedule:json ports:str interfaces:str groups:json
	return 0
}
