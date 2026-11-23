#!/bin/bash /etc/ikcommon
# runhooks抓包插件 service.sh
# 三层架构:
#   层1 iptables(本脚本): 按设备(MAC/IP)+接口/VLAN+端口+时段 精确引流, 绝不全局
#   层2 引擎 scope.json:  按域名+目标IP+GEOIP 决定是否 MITM 解密
#   层3 引擎记录层:       按路径/方法/UA/JA3/内容类型/大小 决定是否保存
PLUGIN_NAME=runhooks
# [storage-anchor] 环境变量由框架 helper 解析(存储卷上的 config.sh / p5 默认值),
# 旧固件(无 helper)回退 sda3 引导文件
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null

# [self-locate] 用脚本自身位置反推插件安装目录, 不盲信 config.sh 的 EXT_PLUGIN_INSTALL_DIR。
# 实踩: 用户把插件存储从 /etc/mnt/plugin_store 改成 /etc/disk_user/plugins 后,
# /usr/ikuai/function/plugin_runhooks 这个软链接和 config.sh 仍指向旧位置, 启动报
# ".../bins/runhooks-x86_64: No such file or directory", 而文件其实在新位置完好无损
# —— 现象和"磁盘不足导致解压失败"一模一样, 但原因完全相反(这边是文件在、路径错)。
# 框架是经软链接调用本脚本的, 所以必须先解析链接再取目录(readlink 取一级目标即可,
# install.sh 建链时用的就是绝对路径)。
# 目录关系: <EXT_PLUGIN_INSTALL_DIR>/<PLUGIN_NAME>/script/service.sh
#           ^^^ 要的就是这一级          ^^^ 插件自己的目录(含 bins/)
# 所以脚本目录要往上退两层才是 EXT_PLUGIN_INSTALL_DIR, 退一层是插件目录。
__slf="$0"
if [ -L "$__slf" ]; then __slf=$(readlink "$__slf" 2>/dev/null); fi
[ -n "$__slf" ] || __slf="$0"
__slfdir=$(cd "$(dirname "$__slf")" 2>/dev/null && pwd)
__pdir=$(cd "$__slfdir/.." 2>/dev/null && pwd)        # 插件自己的目录
__idir=$(cd "$__slfdir/../.." 2>/dev/null && pwd)     # EXT_PLUGIN_INSTALL_DIR
if [ -n "$__pdir" ] && [ -f "$__pdir/bins/runhooks-x86_64" ]; then
	[ -n "$__idir" ] && [ "$__idir" != "$EXT_PLUGIN_INSTALL_DIR" ] && EXT_PLUGIN_INSTALL_DIR="$__idir"
	[ -f "$__pdir/../plugins" ] || EXT_PLUGIN_IPK_DIR="$__pdir/../plugins"
fi
unset __slf __slfdir __pdir __idir

BIN=$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/bins/runhooks-x86_64
DATADIR=$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
# [data-dir-fix] 引擎 data-dir(flows/sessions/history/CA/rules/env)必须搬出配置分区:
# EXT_PLUGIN_CONFIG_DIR 恒为 /etc/mnt/plugins/configs(sda3 仅43MB), 而 set_storage 只搬
# INSTALL_DIR/IPK_DIR、不搬 CONFIG_DIR —— 旧代码把引擎 -data-dir 指向 DATADIR, 会话数据
# 全落 sda3, 实测39.3MB直接把配置分区吃到100%, 用户视角就是"插件管理页设了存储路径也没用"。
# 修法(adguard .adguard_work 同款): data-dir 放安装目录同级 .runhooks_data(数据分区),
# 配置类小文件(ports/devices/scope/schedule/groups/flag/pid)仍留配置分区。
# 兜底: 用户若把存储指到 /etc/mnt 下(配置分区), 退到 /etc/log(sda5, 973MB)。
case "$EXT_PLUGIN_INSTALL_DIR" in
	/etc/mnt/*) DATA_DIR=/etc/log/.runhooks_data ;;
	*)          DATA_DIR=$EXT_PLUGIN_INSTALL_DIR/../.runhooks_data ;;
esac
PROXY_PORT=18060       # 显式代理(设备手动设代理 router:18060)
TRANSPARENT_PORT=18062 # 透明监听(iptables REDIRECT 的目标)
API_PORT=18061         # Web 界面/API
# [plugin-proxy] iKuai 加固(Build202611181800 起固件支持): WebUI 原来直接绑
# 0.0.0.0, 局域网内无痕窗口打开 IP:18061 就能看全部抓包内容/改设置——零鉴权,
# 重大隐患。修法: 固件 OpenResty 加同源代理 /plugin_proxy/runhooks/(校验 sess_key
# 后转发到 127.0.0.1), 引擎 WebUI 只绑 127.0.0.1, 页面 iframe 走同源相对路径。
# 旧固件没有该代理, 保持 0.0.0.0 直连(功能优先; 升级固件后自动收紧)。
__PROXY_MIN_BUILD=202611181800
__web_bind_addr()
{
	local __bd
	__bd=$(grep '^BUILD_DATE=' /etc/release 2>/dev/null | cut -d= -f2)
	case "$__bd" in ''|*[!0-9]*) echo 0.0.0.0; return ;; esac
	if [ "$__bd" -ge "$__PROXY_MIN_BUILD" ]; then echo 127.0.0.1; else echo 0.0.0.0; fi
}
# [proxy-bind-security] 显式代理是零鉴权入口, 绝不能监听 0.0.0.0: 通配地址把
# 它绑到本机所有 IPv4 接口, 包括 PPPoE 拨号拿到的公网 IP —— 公网可直接当开放
# 跳板用。默认只绑内网接口地址(排除默认路由出口 WAN/ppp 与回环), 取不到就
# 回退 127.0.0.1, 安全失败, 绝不回退 0.0.0.0。
# 需要在 WAN 侧内网段也直连、或有多个内网口时, 在 ports.conf 写
# PROXY_BIND=<ip> 覆盖(set_proxy_ports 会保留该行); 否则一律自动检测。
__proxy_bind_addr()
{
	[ -n "$PROXY_BIND" ] && { echo "$PROXY_BIND"; return; }
	local wanif ip
	wanif=$(ip -o -4 route show default 2>/dev/null | awk '{print $5}' | head -1)
	for ip in $(ip -o -4 addr show 2>/dev/null | awk -v w="$wanif" '$2 != w && $2 != "lo" {print $4}' | cut -d/ -f1); do
		case "$ip" in
			127.*|169.254.*|0.*|255.*) continue ;;
		esac
		echo "$ip"; return
	done
	echo 127.0.0.1
}
# OpenResty 代理按插件名查此文件找本地端口(启动/改端口时写, 停止时删)
PP_PORT_FILE=/tmp/iktmp/plugin_web/runhooks
# [port-conf] 端口做成页面可配(用户常以为代理端口是 8080——写死曾让他们以为
# 插件没起来)。配置落 ports.conf, set_proxy_ports 保存后重启引擎生效。
PORTS_CONF=$DATADIR/ports.conf
[ -f $PORTS_CONF ] && . $PORTS_CONF 2>/dev/null
PIDFILE=$DATADIR/runhooks.pid
LOGDIR=$EXT_PLUGIN_LOG_DIR/$PLUGIN_NAME
CAPTURE_FLAG=$DATADIR/capture_on     # 抓包总开关
RULES_FLAG=$DATADIR/rules_on         # iptables 规则当前是否生效(调度器维护)
DEVICES_FILE=$DATADIR/devices.json   # 选中的设备 [{ip,mac,name}]
SCOPE_FILE=$DATA_DIR/scope.json      # 引擎层范围(域名/IP/GEOIP/记录层); 放数据分区——订阅域名量大(可达万级), 配置分区(sda3 43MB)放不下
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
mkdir -p $DATADIR $DATA_DIR $LOGDIR $GROUPS_DIR 2>/dev/null

# ============ 引擎 HTTP 通道(统一) ============
# [engine-api] 真机实测: service.sh 到处是裸 wget --method/--body-data, 设备用户态
# 的 busybox wget 不认这两个选项, 请求根本没发出去, 代码却只判响应体空就报
# 「引擎无响应」——「添加订阅」(POST) 与「关抓包后记录层该暂停」(PUT /api/capture)
# 同时受害, 排查方向被误导成引擎没运行(测试报告 D-01 / D-03 / 修复报告 P3)。
# 统一走这里: 优先固件自带 GNU wget(/usr/bin/wget-ssl), 其次 curl(构建机冒烟),
# 最后 busybox wget 的 GET/POST 兜底。所有调用都用 $(...) 捕获输出, 不往 stdout
# 漏东西(show 动作的 stdout 必须是干净 JSON)。
__WGET_SSL=""
[ -x /usr/bin/wget-ssl ] && __WGET_SSL=/usr/bin/wget-ssl

# [engine-api-side-channel] $(...) 是子 shell, 里面算出的 HTTP 状态码与工具 stderr
# 带不出来。用 DATADIR 下两个固定小文件回传给调用方(每次调用先清空), 供 sub_add/
# sub_meta 在 4xx / 无响应时给出诚实归因, 不再把 jq 的 stderr 乱拼进引擎错误文案。
ENGINE_API_CODE_FILE=$DATADIR/.engine_api_code
ENGINE_API_ERR_FILE=$DATADIR/.engine_api_err

# 取本次 __engine_api 调用拿到的 HTTP 状态码(000=没拿到)
__engine_api_code()
{
	local c=""
	[ -f "$ENGINE_API_CODE_FILE" ] && c=$(cat "$ENGINE_API_CODE_FILE" 2>/dev/null)
	case "$c" in [0-9][0-9][0-9]) printf '%s' "$c" ;; *) printf '000' ;; esac
}

# 取本次 __engine_api 调用的工具 stderr(截断, 只用于如实回报)
__engine_api_err()
{
	local e=""
	[ -f "$ENGINE_API_ERR_FILE" ] && e=$(tr '\n' ' ' < "$ENGINE_API_ERR_FILE" 2>/dev/null | head -c 200)
	printf '%s' "$e"
}

# 引擎 4xx 业务原文: JSON 取 .error/.message, http.Error 的纯文本退化到原文。
__engine_err_text()
{
	local raw=$1 txt=""
	[ -z "$raw" ] && return 0
	txt=$(printf '%s' "$raw" | jq -r '(.error // .message // empty)' 2>/dev/null)
	[ -z "$txt" ] && txt=$(printf '%s' "$raw" | tr -d '\n' | head -c 200)
	printf '%s' "$txt"
}

__engine_api()
{
	local method=$1 path=$2 body=${3-} tmo=${4:-10}
	local url="http://127.0.0.1:$API_PORT$path" out="" code="000" raw="" errf="$ENGINE_API_ERR_FILE"
	: > "$errf" 2>/dev/null
	: > "$ENGINE_API_CODE_FILE" 2>/dev/null
	if [ -n "$__WGET_SSL" ]; then
		# [4xx-passthrough] GNU wget 默认对 4xx/5xx 静默丢弃响应体(-q -O - 拿不到),
		# 引擎「该订阅地址已存在」这类业务错误曾因此被误报成「引擎 API 未返回内容」。
		# --content-on-error 让错误响应体照常写 stdout; -S 把状态行写进 stderr 供解析。
		# 设备 wget-ssl 支持 --method/--body-data(GNU wget 1.20+), 同版本即有此选项。
		if [ -n "$body" ]; then
			out=$($__WGET_SSL -q -S -O - -T $tmo --content-on-error \
				--method=$method --body-data="$body" \
				--header="Content-Type: application/json" "$url" 2>"$errf")
		else
			out=$($__WGET_SSL -q -S -O - -T $tmo --content-on-error \
				--method=$method "$url" 2>"$errf")
		fi
		# 老 wget 不认 --content-on-error: 去掉它重试一次(2xx 仍正常, 4xx 由
		# 调用方的订阅列表反查兜底), 并把「不支持该选项」如实留在 stderr。
		if [ -z "$out" ] && grep -qi 'unrecognized option\|invalid option\|unknown option' "$errf" 2>/dev/null; then
			: > "$errf"
			if [ -n "$body" ]; then
				out=$($__WGET_SSL -q -S -O - -T $tmo --method=$method --body-data="$body" \
					--header="Content-Type: application/json" "$url" 2>"$errf")
			else
				out=$($__WGET_SSL -q -S -O - -T $tmo --method=$method "$url" 2>"$errf")
			fi
		fi
		code=$(grep '^ *HTTP/' "$errf" 2>/dev/null | tail -1 | awk '{print $2}')
		case "$code" in [0-9][0-9][0-9]) ;; *) code="000" ;; esac
	elif command -v curl >/dev/null 2>&1; then
		# curl 用 -w 把状态码附在末行, 拆开后 body 原样回传(4xx 也不吞)。
		if [ -n "$body" ]; then
			raw=$(curl -sS --max-time $tmo -X $method -H "Content-Type: application/json" \
				-d "$body" -w $'\n%{http_code}' "$url" 2>"$errf")
		else
			raw=$(curl -sS --max-time $tmo -X $method -w $'\n%{http_code}' "$url" 2>"$errf")
		fi
		case "$raw" in
			*$'\n'*) code=${raw##*$'\n'}; out=${raw%$'\n'*} ;;
			*) code="000"; out="" ;;
		esac
		case "$code" in [0-9][0-9][0-9]) ;; *) code="000" ;; esac
	else
		# busybox wget 只有 GET 与 --post-data 的 POST, PUT/DELETE 无法表达。
		case "$method" in
			GET)  out=$(wget -q -O - -T $tmo "$url" 2>"$errf") ;;
			POST) [ -n "$body" ] && out=$(wget -q -O - -T $tmo --post-data="$body" "$url" 2>"$errf") ;;
			*) echo "引擎 API $method $path 失败: 本机无 GNU wget 或 curl" >&2; return 1 ;;
		esac
	fi
	printf '%s' "$code" > "$ENGINE_API_CODE_FILE" 2>/dev/null
	printf '%s' "$out"
	return 0
}

# 引擎记录层当前是否暂停(返回 true/false; 拿不到返回空)
__engine_paused()
{
	local raw=$(__engine_api GET /api/capture "" 5 2>/dev/null)
	[ -n "$raw" ] && echo "$raw" | jq -r '.paused // empty' 2>/dev/null
	return 0
}

# 把记录层暂停态对齐抓包总开关, 并回读强校验(不静默相信 PUT 的结果)
__align_capture_pause()
{
	local want=true cur i on msg
	[ -f $CAPTURE_FLAG ] && want=false
	[ "$want" = "false" ] && on=1 || on=0
	for i in 1 2 3; do
		__engine_capture $on
		cur=$(__engine_paused)
		[ "$cur" = "$want" ] && return 0
		sleep 1
	done
	msg="警告: 引擎记录层暂停态未对齐总开关(期望 paused=$want, 实际=${cur:-无响应})"
	echo "$msg"
	echo "$msg" >> $LOGDIR/runhooks.log 2>/dev/null
	return 1
}

# [watchdog] 引擎「期望运行」标志: start 写、stop 删。
RUNNING_FLAG=$DATADIR/engine_wanted


# [data-dir-fix] 一次性迁移: 旧版引擎数据在配置分区(DATADIR), 搬到 DATA_DIR。
# 用 mv 不用 cp —— 搬完 sda3 才真腾出空间。CA 必须搬: 用户客户端上装的是旧 CA 根证书,
# 删了重建会导致全部设备证书不受信、要逐个重装。rules/env 是用户建的改写规则/REST 环境,
# 同样必须保留。sessions/history 是抓包记录, 一并搬(用户可能要回看)。
__migrate_data_dir()
{
	local f moved=0
	for f in sessions.json history.json token.json rules.json env.json \
		ca.pem ca.key mitmproxy-ca-cert.pem mitmproxy-ca-cert.p12 ca-cert.pem; do
		if [ -f "$DATADIR/$f" ] && [ ! -e "$DATA_DIR/$f" ]; then
			mv -f "$DATADIR/$f" "$DATA_DIR/$f" 2>/dev/null && moved=1
		fi
	done
	# geoip 符号链接改链到 DATA_DIR(引擎从 data-dir 读 geoip.csv), 旧链接删掉重建即可
	[ -L "$DATADIR/geoip.csv" ] && rm -f "$DATADIR/geoip.csv"
	[ $moved -eq 1 ] && { command -v debug >/dev/null 2>&1 && debug "runhooks 引擎数据已迁移: $DATADIR -> $DATA_DIR(释放配置分区)"; }
	return 0
}

# ============ 层1: iptables 规则生成(按维度精确匹配, 幂等) ============

# 从 devices.json 读设备列表, 生成 nat REDIRECT 规则(每设备一条)
# 匹配优先级: MAC 优先(DHCP 换 IP 不失效), 无 MAC 用 -s IP
iptables_add()
{
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
	iptables -t nat -N RUNHOOKS_REDIRECT 2>/dev/null
	iptables -t nat -C PREROUTING -j RUNHOOKS_REDIRECT 2>/dev/null || iptables -t nat -I PREROUTING -j RUNHOOKS_REDIRECT
	iptables -t nat -F RUNHOOKS_REDIRECT
	iptables -N RUNHOOKS_QUIC 2>/dev/null
	iptables -C FORWARD -j RUNHOOKS_QUIC 2>/dev/null || iptables -I FORWARD -j RUNHOOKS_QUIC
	iptables -F RUNHOOKS_QUIC

	# 排除路由器自身全部 IP(否则 LAN 访问路由器管理页也会被截)。
	# [bugfix] 原来只排 ip route 第一条 src(实际是默认路由的 WAN IP, 如
	# 10.10.10.6), LAN IP(192.168.9.1)反倒没排除——改为枚举本机所有 IPv4
	local lip
	for lip in $(ip -o -4 addr show 2>/dev/null | awk '{print $4}' | cut -d/ -f1); do
		iptables -t nat -A RUNHOOKS_REDIRECT -d $lip -j RETURN
	done

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
		iptables -t nat -A RUNHOOKS_REDIRECT $ifm -p tcp -m multiport --dports $ports $match -j REDIRECT --to-ports $TRANSPARENT_PORT
		# QUIC(UDP443) 阻断, 强制浏览器回退 TCP(否则 HTTP/3 绕过 MITM)
		iptables -A RUNHOOKS_QUIC $ifm -p udp --dport 443 $match -j DROP
		count=$((count+1))
	done < <(echo "$devs" | jq -c '.[]' 2>/dev/null)
	touch $RULES_FLAG
	echo "规则已生效: $count 台设备"
	return 0
}

iptables_del()
{
	iptables -t nat -D PREROUTING -j RUNHOOKS_REDIRECT 2>/dev/null
	iptables -t nat -F RUNHOOKS_REDIRECT 2>/dev/null
	iptables -t nat -X RUNHOOKS_REDIRECT 2>/dev/null
	iptables -D FORWARD -j RUNHOOKS_QUIC 2>/dev/null
	iptables -F RUNHOOKS_QUIC 2>/dev/null
	iptables -X RUNHOOKS_QUIC 2>/dev/null
	rm -f $RULES_FLAG
	return 0
}

# [tz-fix] 路由器本地时区(小时, 8=UTC+8)。设备 shell 时钟是 UTC(系统时钟 UTC、
# 环境无 TZ), 而用户按 UI 显示的本地时间填时段 —— 直接比 UTC 的话时段整体偏移
# time_zone 小时(实测: 填 22:58-23:08, 设备 14:58 UTC 永远不在时段内, 规则
# 到点不上)。从配置库 global_config.time_zone 读(固件自己的脚本也这么读),
# 读不到兜底 8。
__router_tz()
{
	local tz=$(sqlite3 ${IK_DB_CONFIG:-/etc/mnt/ikuai/config.db} \
		"select time_zone from global_config limit 1" 2>/dev/null)
	case "$tz" in ''|*[!0-9-]*) tz=8;; esac
	[ "$tz" -ge -12 ] && [ "$tz" -le 14 ] || tz=8
	echo "$tz"
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
	# [tz-fix] 把 UTC 现在时间迁到路由器本地时间再比(见 __router_tz)。
	# 纯算术不依赖 libc 的 TZ 支持; 10# 防 08/09 被当八进制。
	local tz=$(__router_tz)
	local __h=$(date -u +%H) __m=$(date -u +%M)
	local __t=$(( (10#$__h * 60 + 10#$__m + tz * 60 + 1440) % 1440 ))
	local now
	printf -v now '%02d:%02d' $((__t / 60)) $((__t % 60))
	# 星期同样按本地日期算: 平移后的 epoch 天数 +3(1970-01-01 是周四) 取模
	local dow=$(( ( ($(date -u +%s) + tz * 3600) / 86400 + 3 ) % 7 + 1 )) # 1=周一..7=周日
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

# [watchdog] 看门狗循环: 原来以「PIDFILE 里的进程还活着」为条件, 引擎一旦自退
# (真机 O-03: 周期性 502, 日志 shutdown reason=context canceled), 循环立刻退出
# 并撤掉 iptables, 之后再没有人把引擎拉起来 —— 页面一直停在
# 「plugin web ui not available」, 只能用户手动点启动。改为以 RUNNING_FLAG 为条件:
#   ① 用 /api/health 探活(kill -0 对僵尸仍为真, 502 时 PID 在但已不服务), 自退就重新拉起;
#   ② 每轮持续对齐记录层暂停态(修启动竞态: capture=0 却 enginePaused=0);
#   ③ 照旧按时段/总开关协调 iptables。
watchdog_loop()
{
	while [ -f $RUNNING_FLAG ]; do
		if ! wget -q -O /dev/null -T3 http://127.0.0.1:$API_PORT/api/health 2>/dev/null; then
			[ -f $PIDFILE ] && kill -9 $(cat $PIDFILE) 2>/dev/null
			rm -f $PIDFILE
			__spawn_engine >/dev/null 2>&1
		fi
		if [ -f $PIDFILE ]; then
			local want=true cur on
			[ -f $CAPTURE_FLAG ] && want=false
			[ "$want" = "false" ] && on=1 || on=0
			cur=$(__engine_paused)
			if [ -n "$cur" ] && [ "$cur" != "$want" ]; then
				__engine_capture $on >/dev/null 2>&1
			fi
		fi
		if [ -f $CAPTURE_FLAG ] && in_schedule; then
			[ -f $RULES_FLAG ] || iptables_add >/dev/null 2>&1
		else
			[ -f $RULES_FLAG ] && iptables_del >/dev/null 2>&1
		fi
		sleep 10
	done
	iptables_del >/dev/null 2>&1
}

# 拉起引擎进程本身(不含看门狗): start 与 watchdog 共用, 避免递归启调度循环。
__spawn_engine()
{
	mkdir -p $DATADIR $DATA_DIR $LOGDIR $GROUPS_DIR
	# GEOIP 数据在大容量分区(配置分区空间不足), 保持软链到引擎 dataDir
	[ -s $EXT_PLUGIN_INSTALL_DIR/../.geoip_cache/geoip.csv ] && ln -sf $EXT_PLUGIN_INSTALL_DIR/../.geoip_cache/geoip.csv $DATA_DIR/geoip.csv
	# [bin-preflight] 安装时 tar 解压错误被框架静默(2>/dev/null 且不查退出码),
	# 磁盘不足时小文件(html/script)解压成功、唯独末尾 19MB 的 bins/ 失败,
	# 结果插件页正常渲染但一起机就 "No such file or directory", 完全看不出原因。
	if [ ! -f "$BIN" ]; then
		echo "引擎启动失败: 找不到引擎二进制 $BIN"
		echo "  插件目录内容: $(ls -la $EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME/ 2>/dev/null | tr '\n' ' ')"
		__alt=""
		for __d in /etc/disk_user/*/plugins_installed /etc/mnt/plugin_store/plugins_installed /etc/log/plugins_installed; do
			[ -d "$__d" ] || continue
			for __p in "$__d"/*; do
				[ -d "$__p" ] || continue
				[ -f "$__p/bins/runhooks-x86_64" ] || continue
				[ "$__p" = "$EXT_PLUGIN_INSTALL_DIR/$PLUGIN_NAME" ] && continue
				__alt="$__p"
			done
		done
		if [ -n "$__alt" ]; then
			echo "  ★ 在 $__alt 找到了完整的插件(含二进制)!"
			echo "    原因: 插件存储路径改过, 但 /usr/ikuai/function/plugin_runhooks 软链接"
			echo "          和 config.sh 还指向旧位置 $EXT_PLUGIN_INSTALL_DIR"
			echo "    处理: 重新安装/更新一次本插件即可(install.sh 会重建软链接), 无需卸载数据"
		else
			echo "  常见原因: ① 安装时磁盘空间不足, tar 解压到末尾的 bins/ 时失败"
			echo "            (框架会静默解压错误, 所以安装显示成功但文件不全)"
			echo "            ② 插件包没下载完整"
			echo "  处理: 到「系统管理 → 磁盘管理」看插件存储分区剩余空间, 清理后重装/更新插件"
		fi
		df -h 2>/dev/null | head -20
		return 1
	fi
	if [ ! -x "$BIN" ]; then
		echo "引擎启动失败: $BIN 存在但没有可执行权限"
		ls -la "$BIN" 2>/dev/null
		chmod +x "$BIN" 2>/dev/null
		[ -x "$BIN" ] || { echo "  chmod 失败, 请检查文件系统是否只读"; return 1; }
		echo "  已自动补上可执行权限, 重试启动"
	fi
	__migrate_data_dir
	# [fd-limit] 抬高 nofile 兜底(引擎侧另有 IdleTimeout 回收空闲连接)
	for __n in 65536 32768 8192 4096; do ulimit -n $__n 2>/dev/null && break; done
	local __web_bind __proxy_bind
	__web_bind=$(__web_bind_addr)
	__proxy_bind=$(__proxy_bind_addr)
	# 显式代理零鉴权 -> 只绑内网地址; 透明端口 18062 不是代理入口, 只接
	# iptables REDIRECT 按设备/接口/端口精确引流过来的连接(公网直连会因取不到
	# SO_ORIGINAL_DST 被引擎丢弃), 保持 0.0.0.0 以免 REDIRECT 目的地址不匹配。
	$BIN -proxy-addr $__proxy_bind:$PROXY_PORT -transparent-addr 0.0.0.0:$TRANSPARENT_PORT \
		-api-addr $__web_bind:$API_PORT \
		-data-dir $DATA_DIR -transparent -scope-file $SCOPE_FILE \
		>>$LOGDIR/runhooks.log 2>&1 </dev/null &
	echo $! > $PIDFILE
	# [plugin-proxy] 落端口文件: OpenResty 代理按插件名查此文件找本地端口
	if [ "$__web_bind" = "127.0.0.1" ]; then
		mkdir -p /tmp/iktmp/plugin_web 2>/dev/null
		echo $API_PORT > $PP_PORT_FILE 2>/dev/null
	fi
	sleep 1
	wget -q -O /dev/null -T3 http://127.0.0.1:$API_PORT/api/ca.pem 2>/dev/null
	# [capture-pause-verify] 引擎每次启动 paused 默认 false; 总开关关着时必须变成
	# true, 且必须回读确认。原实现只发一次 PUT 不校验, 在频繁重启竞态下经常没
	# 生效 → 用户看到「抓包已关」但流量还在涨(D-03)。
	__align_capture_pause
	return 0
}

start()
{
	if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
		# 引擎在跑: 仍要确认看门狗在(升级/异常重启后可能只剩引擎没有看门狗)
		if [ -f $SCHED_PIDFILE ] && kill -0 $(cat $SCHED_PIDFILE) 2>/dev/null; then
			return 0
		fi
	else
		touch $RUNNING_FLAG
		__spawn_engine || { rm -f $RUNNING_FLAG; return 1; }
		if ! kill -0 $(cat $PIDFILE) 2>/dev/null; then
			echo "引擎启动失败, 日志尾部:"
			tail -3 $LOGDIR/runhooks.log 2>/dev/null
			rm -f $PIDFILE $RUNNING_FLAG
			return 1
		fi
	fi
	touch $RUNNING_FLAG
	[ -f $SCHED_PIDFILE ] && kill $(cat $SCHED_PIDFILE) 2>/dev/null
	( watchdog_loop ) >/dev/null 2>&1 </dev/null &
	echo $! > $SCHED_PIDFILE
	return 0
}

stop()
{
	# [watchdog] 先撤「期望运行」标志, 否则看门狗可能在 kill 窗口里又拉起引擎
	rm -f $RUNNING_FLAG
	[ -f $SCHED_PIDFILE ] && kill $(cat $SCHED_PIDFILE) 2>/dev/null
	rm -f $SCHED_PIDFILE
	# [plugin-proxy] 停止即摘掉代理端口文件: 文件在=代理可转发, 文件不在=代理
	# 明确报"插件未运行"(502), 不会把请求透给一个死端口
	rm -f $PP_PORT_FILE
	# [stop-wait] kill 只是发信号, 进程退出和端口释放是异步的。原来 stop 发完信号
	# 立即返回, 调用方紧接着 start 时新进程 bind 不上同一端口 → 起即死 → "引擎启动
	# 失败"(2026-11-17 设备实踩: stop→start 竞态, 第二次 start 才成)。这里等到进程
	# 真消失(最多 5s)再返回, 让 stop/start 成为可依赖的原子对。
	if [ -f $PIDFILE ]; then
		local __p=$(cat $PIDFILE)
		kill $__p 2>/dev/null
		local __i=0
		while kill -0 $__p 2>/dev/null && [ $__i -lt 50 ]; do
			sleep 0.1
			__i=$((__i+1))
		done
		kill -9 $__p 2>/dev/null
	fi
	rm -f $PIDFILE
	iptables_del
	return 0
}

# [param-b64] 框架 /Action/call 的参数值禁含 引号/空格/$/&/;/|/\  (ikngx.lua 的
# __rest_check_illegality_char), 明文 JSON 必被 2005 Invalid characters 拒掉——
# 页面保存设备/范围/分组/时段曾因此全部静默失败(用户实踩: 设备列表始终空、
# 抓不到包)。统一 base64 传输(仅 [A-Za-z0-9+/=] 全合法), 此处解码;
# 无 _b64 时回退明文参数(chroot 直调/兼容旧调用)。
__param_decode()
{
	local v="${!1}"
	if [ -n "$v" ]; then
		echo "$v" | base64 -d 2>/dev/null && return
	fi
	echo "${!2}"
}

# ============ 配置动作(页面调用) ============

# 总开关: set_capture capture=true|false
# [capture-pause] 总开关必须同时管住引擎的记录层。以前只撤 iptables 透明转发,
# 显式代理(18060)的流量照样进引擎照样落库 —— 用户实踩"关了抓包还在抓, WebUI
# 里流量还在涨"(手动设了代理的设备根本不受 iptables 规则影响)。
__engine_capture()
{
	local on=$1 paused=true
	[ "$on" = "1" ] && paused=false
	__engine_api PUT /api/capture "{\"paused\":$paused}" 5 >/dev/null 2>&1
	return 0
}

set_capture()
{
	if [ "$capture" = "true" ]; then
		# 引擎没跑先拉起来: 规则指向没监听的端口会把勾选设备的 80/443 全弄断
		if [ ! -f $PIDFILE ] || ! kill -0 $(cat $PIDFILE) 2>/dev/null; then
			start || { echo "引擎未运行且启动失败, 抓包无法开启"; return 1; }
		fi
		touch $CAPTURE_FLAG
		__engine_capture 1
		if in_schedule; then
			iptables_add
		else
			echo "抓包已开启, 但当前不在设定时段内, 规则未生效(到点自动生效)"
		fi
	else
		rm -f $CAPTURE_FLAG
		iptables_del
		__engine_capture 0
		echo "抓包已关闭, 网络规则已撤, 引擎已暂停记录"
	fi
	return 0
}

# 设备选择: set_devices devices='[{"ip":"..","mac":"..","name":".."}]'
set_devices()
{
	local dj=$(__param_decode devices_b64 devices)
	echo "$dj" | jq -e 'type == "array"' >/dev/null 2>&1 || { echo "devices 参数必须是 JSON 数组"; return 1; }
	echo "$dj" | jq '.' > $DEVICES_FILE
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
	local sc=$(__param_decode scope_b64 scope)
	echo "$sc" | jq -e 'type == "object"' >/dev/null 2>&1 || { echo "scope 参数必须是 JSON 对象"; return 1; }
	echo "$sc" | jq '.' > $SCOPE_FILE
	# SIGHUP 热重载(抓包不中断)
	if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
		kill -HUP $(cat $PIDFILE) 2>/dev/null
	fi
	echo "捕获范围已更新并热重载"
	return 0
}

# 引擎端口设置(页面可配): set_proxy_ports proxy=8080 api=18061 transparent=18062
# 端口在引擎启动命令行里, 改端口必须重启引擎; 透明端口变的话规则也要重建
set_proxy_ports()
{
	local p=${proxy:-$PROXY_PORT} a=${api:-$API_PORT} t=${transparent:-$TRANSPARENT_PORT}
	local x
	for x in "$p" "$a" "$t"; do
		echo "$x" | grep -qE '^[0-9]+$' || { echo "端口必须是非负整数: $x"; return 1; }
		[ "$x" -ge 1 ] && [ "$x" -le 65535 ] || { echo "端口超出范围(1-65535): $x"; return 1; }
	done
	if [ "$p" = "$a" ] || [ "$p" = "$t" ] || [ "$a" = "$t" ]; then
		echo "三个端口不能相同"; return 1
	fi
	# [proxy-bind-security] 保留管理员显式指定的绑定地址, 别被端口重写冲掉
	local __keep_bind=""
	[ -f $PORTS_CONF ] && __keep_bind=$(grep '^PROXY_BIND=' $PORTS_CONF 2>/dev/null | head -1)
	cat > $PORTS_CONF <<-EOT
PROXY_PORT=$p
API_PORT=$a
TRANSPARENT_PORT=$t
$__keep_bind
EOT
	# [ports-reread] 端口变量是**脚本加载时** source 进内存的(见文件头 PORTS_CONF 那行),
	# 光写文件不重读的话下面 start 用的还是旧端口 —— 引擎确实重启了但还听在老端口上,
	# 而框架下次重新跑脚本(用户手动点启动/开机)又读到新端口, 表现就是"改端口当时不
	# 生效, 手动点一次启动才生效"(2026-11-17 测试 agent 实踩, 一度误记成"不会自动重启")。
	. $PORTS_CONF 2>/dev/null
	# 重启引擎使新端口生效(端口在引擎启动命令行里)
	stop
	sleep 1
	start
	# 规则热更新(透明端口可能变了)
	if [ -f $CAPTURE_FLAG ] && in_schedule; then iptables_add; fi
	echo "端口已更新: 代理=$p API/UI=$a 透明=$t(引擎已重启)"
	return 0
}

# 端口: set_ports ports='80,443,8080'
set_ports()
{
	echo "$ports" | grep -qE '^[0-9]+(,[0-9]+)*$' || { echo "ports 格式: 80,443"; return 1; }
	echo "$ports" > $DATADIR/ports
	# [ports-restart] 端口是引擎的**启动参数**, 只落文件不重启的话引擎还在旧端口上听,
	# 而 iptables REDIRECT 已经按新端口改了 → 透明流量打到一个没人的端口, 页面探活
	# 也显示"已停止"(用户视角: 改了端口引擎就死了)。文档承诺"改端口自动重启、新端口
	# 立即可用", 这里补上: 在跑才停+起, 不在跑只落配置等下次 start 生效。
	if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
		stop >/dev/null 2>&1
		start >/dev/null 2>&1
		echo "端口已更新并重启引擎: $ports"
	else
		echo "端口已更新(引擎未运行, 下次启动生效): $ports"
	fi
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
	local sc=$(__param_decode schedule_b64 schedule)
	echo "$sc" | jq -e 'type == "object"' >/dev/null 2>&1 || { echo "schedule 参数必须是 JSON 对象"; return 1; }
	echo "$sc" | jq '.' > $SCHED_FILE
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
	local nm=$(__param_decode name_b64 name)
	local dj=$(__param_decode devices_b64 devices)
	[ -z "$nm" ] && { echo "缺少 name"; return 1; }
	echo "$nm" | grep -q '/' && { echo "name 不能含 /"; return 1; }
	echo "$dj" | jq -e 'type == "array"' >/dev/null 2>&1 || { echo "devices 必须是 JSON 数组"; return 1; }
	echo "$dj" | jq '.' > "$GROUPS_DIR/$nm.json"
	echo "分组已保存: $nm"
	return 0
}

del_group()
{
	local nm=$(__param_decode name_b64 name)
	[ -f "$GROUPS_DIR/$nm.json" ] && rm -f "$GROUPS_DIR/$nm.json"
	echo "分组已删除: $nm"
	return 0
}

# GEOIP 数据更新(下载 db-ip lite CSV)
update_geoip()
{
	echo "正在下载 GEOIP 数据..."
	# [disk-fix] 配置分区(实测43MB, 96%满, 剩1.8MB)放不下11MB的CSV, 写盘失败
	# 曾被误判为"下载失败"。改到安装/数据分区(实测2.8G, 空闲2.5G)落盘, 再软链
	# 到引擎 dataDir(引擎从 dataDir/geoip.csv 读, Go os.Open 跟随 symlink)。
	# [wget-arg-order] busybox wget 不重排参数: 选项必须全在 URL 之前, 放到
	# URL 后面会被当成额外 URL 导致秒败(曾表现为"两个源均不可达")。
	# 超时120s: 设备到 CDN 实测 ~550KB/s, 11MB 需 ~20s, 留足余量。
	# 放安装目录的同级(.geoip_cache): 升级会整体替换插件安装目录(实测升级后
	# 安装目录内的 geoip.csv 被冲掉), 同级目录不受升级影响, 同在大容量分区
	local GEO_STORE=$EXT_PLUGIN_INSTALL_DIR/../.geoip_cache
	mkdir -p $GEO_STORE 2>/dev/null
	local ok=0 __u
	for __u in "$GEOIP_URL" "$GEOIP_URL2"; do
		if wget -q -T 120 -O $GEO_STORE/geoip.csv.tmp "$__u" 2>/dev/null && [ -s $GEO_STORE/geoip.csv.tmp ]; then
			ok=1
			break
		fi
		rm -f $GEO_STORE/geoip.csv.tmp
	done
	if [ "$ok" = "1" ]; then
		mv -f $GEO_STORE/geoip.csv.tmp $GEO_STORE/geoip.csv
		ln -sf $GEO_STORE/geoip.csv $DATA_DIR/geoip.csv
		# SIGHUP 让引擎重载
		[ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null && kill -HUP $(cat $PIDFILE) 2>/dev/null
		local lines=$(wc -l < $DATA_DIR/geoip.csv 2>/dev/null)
		echo "GEOIP 数据已更新($lines 条)"
		return 0
	fi
	rm -f $GEO_STORE/geoip.csv.tmp
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
	__engine_api DELETE /api/flows "" 10 >/dev/null 2>&1
	return 0
}

# ============ 规则订阅(QuanX/Surge/纯列表 → 引擎适配层) ============
# 订阅数据全部落引擎层: scope.sub.json / rules.sub.json / scripts/sub-*.js,
# 与用户手写配置物理分离(见 /opt/runhooks-engine internal/subscription)。
# 页面经本 func 中转(引擎 API 只绑 127.0.0.1), 参数走 base64(URL 常含 ?&=,
# 明文必被框架 __rest_check_illegality_char 拒掉)。

# ---- 设备固件 jq 形态适配(v1.1.6 / P1 真机回归根因) ----
# v1.1.5 复测报告 §4.2 实证: 设备固件自带 jq 与构建机 GNU jq 形态不一致 ——
# jq -nc --argjson ... 在设备上恒返回空(连纯字母 URL 都失败), 而 jq -c / jq -r
# 正常工作。上一版把 __engine_api 统一通道修好了, 却没同步修 body 构造层,
# 于是「添加订阅」仍落到 [ -n "$body" ] 判空分支, 甩出「订阅地址含非法字符」
# 的假归因。这里三管齐下:
#   ① 主路径去掉 --argjson, 用 --arg + ($i|tonumber)(jq 1.4+ 皆支持);
#   ② 设备 jq 连 -n/--arg 整条都不支持或返回空时, 走纯 shell 受控 printf
#      构造, 不依赖任何 jq, 且对引号/反斜杠/控制字符做 JSON 转义;
#   ③ 构造失败时把 jq 真实 stderr 回显, 不再误导归因。
__json_escape()
{
	local s=$1 out="" i c o
	for (( i=0; i<${#s}; i++ )); do
		c=${s:i:1}
		if   [ "$c" = '\' ];    then out="$out"'\\'
		elif [ "$c" = '"' ];    then out="$out"'\"'
		elif [ "$c" = $'\n' ];  then out="$out"'\n'
		elif [ "$c" = $'\r' ];  then out="$out"'\r'
		elif [ "$c" = $'\t' ];  then out="$out"'\t'
		else
			o=$(printf '%d' "'$c" 2>/dev/null)
			if [ -n "$o" ] && [ "$o" -lt 32 ] 2>/dev/null; then
				out="$out"$(printf '\\u%04x' "$o")
			else
				out="$out$c"
			fi
		fi
	done
	printf '%s' "$out"
}

# 跑一条 jq -nc 构造: stdout 只保留以 { 开头的合法 JSON, stderr 落到 $1 文件,
# 供调用方在 body 为空时回显真因。命令替换是子 shell, 变量带不出来, 用文件传。
__jq_nc()
{
	local errf=$1; shift
	: > "$errf"
	jq -nc "$@" 2>"$errf"
}

# 中转层实在拿不到 4xx 响应体(老 wget / busybox wget)时, 用引擎订阅列表反查
# 重复地址回业务文案, 而不是甩「引擎没运行」。仅在 sub_add 的空响应兜底分支调用。
__sub_duplicate_text()
{
	local want=$1 raw="" esc="" esc2="" hit=""
	raw=$(__engine_api GET /api/subscriptions "" 10 2>/dev/null)
	[ -z "$raw" ] && return 0
	esc=$(__json_escape "$want")
	# 优先用 jq 按值比较, 绕开 Go json.Encoder 把 & < > 写成 \u00xx 的字面差异;
	# 只用管道过滤, 不用 --arg/-n, 设备裁剪版 jq 也能跑。
	hit=$(printf '%s' "$raw" | jq -r ".subs[]? | select(.url == \"$esc\") | .id" 2>/dev/null)
	[ -n "$hit" ] && { printf '%s' "该订阅地址已存在"; return 0; }
	# jq 缺位/异常时退化到字面匹配: 原样 + Go HTML 转义形态各试一次。
	esc2=${esc//&/\\u0026}; esc2=${esc2//</\\u003c}; esc2=${esc2//>/\\u003e}
	if printf '%s' "$raw" | grep -qF "\"url\":\"$esc\"" || \
	   printf '%s' "$raw" | grep -qF "\"url\":\"$esc2\""; then
		printf '%s' "该订阅地址已存在"
	fi
	return 0
}

# sub_add url_b64=<b64> [name_b64=<b64>] [format=auto] [interval_min=0]
sub_add()
{
	local u=$(__param_decode url_b64 url)
	local nm=$(__param_decode name_b64 name)
	[ -z "$u" ] && { echo "缺少订阅地址"; return 1; }
	# [interval-guard] 规整成合法非负整数, 别让空串/脏值进 jq 或进数字字段。
	local iv="${interval_min:-0}"
	echo "$iv" | grep -qE '^[0-9]+$' || iv=0
	local f="${format:-auto}"
	local errf="$DATADIR/.sub_jq_err.$$" jqerr="" body=""
	body=$(__jq_nc "$errf" --arg u "$u" --arg n "$nm" --arg f "$f" --arg i "$iv" \
		'{url:$u, name:$n, format:$f, interval_min:($i|tonumber)}')
	[ -s "$errf" ] && jqerr=$(tr '\n' ' ' < "$errf")
	rm -f "$errf"
	case "$body" in '{'*) ;; *) body="" ;; esac
	# [jq-fallback] 设备裁剪版 jq 或 jq 缺位时, 纯 shell 受控构造兜底。
	if [ -z "$body" ]; then
		body=$(printf '{"url":"%s","name":"%s","format":"%s","interval_min":%s}' \
			"$(__json_escape "$u")" "$(__json_escape "$nm")" \
			"$(__json_escape "$f")" "$iv")
		# [jqerr-scope] 纯 shell 兜底已成功构造 body, jq 的 stderr 与本次请求无关,
		# 必须清空——否则会被拼进引擎错误文案, 形成「引擎没跑 + jq 又失败」双重误导。
		jqerr=""
	fi
	if [ -z "$body" ]; then
		echo "添加订阅失败: 参数构造失败(jq: ${jqerr:-设备 jq 未返回内容, 已尝试无 jq 兜底})"
		return 1
	fi
	local res code="000" toolerr="" dup=""
	res=$(__engine_api POST /api/subscriptions "$body" 30 2>/dev/null)
	code=$(__engine_api_code)
	toolerr=$(__engine_api_err)
	# [empty-res-first] 空响应体必须先单独处理: jq -e '.id' 对空输入返回 0
	# (无输出即成功), 先跑成功判定会把「引擎没回内容」误判成「订阅已添加并生效」。
	if [ -z "$res" ]; then
		# 只会发生在工具确实拿不到 4xx body(老 wget / busybox wget)。
		# 先按引擎订阅列表反查重复地址, 尽量回业务文案; 再不行如实报 HTTP 状态与工具 stderr。
		dup=$(__sub_duplicate_text "$u")
		[ -n "$dup" ] && { echo "添加订阅失败: $dup"; return 1; }
		echo "添加订阅失败: 引擎 API(端口 $API_PORT) 未返回响应体(HTTP $code)${toolerr:+(工具 stderr: $toolerr)}"
		return 1
	fi
	# 设备裁剪版 jq 可能不支持 -e, 退化为结构匹配, 保证无 jq 时也能判成功。
	if echo "$res" | jq -e '.id' >/dev/null 2>&1 || \
	   echo "$res" | grep -q '"id"[[:space:]]*:'; then
		echo "订阅已添加并生效"
		return 0
	fi
	# [business-error] __engine_api 已把 4xx 响应体透传回来: 引擎业务原文(如
	# 「该订阅地址已存在(xxx)」)直接回给页面, 不再落到「引擎无响应」。
	echo "添加订阅失败: $(__engine_err_text "$res")"
	return 1
}

# sub_del id=sub-xxx
sub_del()
{
	[ -z "$id" ] && { echo "缺少订阅 id"; return 1; }
	echo "$id" | grep -qE '^sub-[A-Za-z0-9_-]+$' || { echo "id 格式不正确"; return 1; }
	local res
	res=$(__engine_api DELETE /api/subscriptions/$id "" 15 2>/dev/null)
	[ -n "$res" ] || { echo "删除失败: 引擎无响应"; return 1; }
	echo "订阅已删除"
	return 0
}

# sub_toggle id=sub-xxx enabled=true|false
sub_toggle()
{
	[ -z "$id" ] && { echo "缺少订阅 id"; return 1; }
	local en=false
	[ "$enabled" = "true" ] && en=true
	local res
	res=$(__engine_api PUT /api/subscriptions/$id "{\"enabled\":$en}" 15 2>/dev/null)
	[ -n "$res" ] || { echo "操作失败: 引擎无响应"; return 1; }
	echo "订阅已$( [ "$en" = "true" ] && echo 启用 || echo 暂停 )"
	return 0
}

# sub_update id=sub-xxx (立即重新拉取)
sub_update()
{
	[ -z "$id" ] && { echo "缺少订阅 id"; return 1; }
	local res
	res=$(__engine_api POST /api/subscriptions/$id/update "" 60 2>/dev/null)
	if [ -z "$res" ]; then
		echo "更新失败: 引擎无响应"
		return 1
	fi
	if echo "$res" | jq -e '.id' >/dev/null 2>&1; then
		echo "订阅已更新"
		return 0
	fi
	echo "更新失败: $res"
	return 1
}

# sub_meta id=sub-xxx [name_b64=<b64>] [interval_min=N]
sub_meta()
{
	[ -z "$id" ] && { echo "缺少订阅 id"; return 1; }
	local nm=$(__param_decode name_b64 name)
	# 与 sub_add 同源(v1.1.6): 去掉 --argjson, 设备 jq 异常时走纯 shell 兜底,
	# 失败回显 jq 真实 stderr。旧版同样用 jq -nc --argjson, 复测 §4.2 实测
	# 「保存失败: 引擎无响应」也是这层 body 构造返回空导致的, 一并修掉。
	local iv="${interval_min:--1}"
	echo "$iv" | grep -qE '^-?[0-9]+$' || iv=-1
	local errf="$DATADIR/.sub_jq_err.$$" jqerr="" body=""
	body=$(__jq_nc "$errf" --arg n "$nm" --arg i "$iv" \
		'{name:$n, interval_min:($i|tonumber)}')
	[ -s "$errf" ] && jqerr=$(tr '\n' ' ' < "$errf")
	rm -f "$errf"
	case "$body" in '{'*) ;; *) body="" ;; esac
	if [ -z "$body" ]; then
		body=$(printf '{"name":"%s","interval_min":%s}' \
			"$(__json_escape "$nm")" "$iv")
		# 同 sub_add: 兜底成功后 jq 的 stderr 与本次请求无关, 清空。
		jqerr=""
	fi
	if [ -z "$body" ]; then
		echo "保存失败: 参数构造失败(jq: ${jqerr:-设备 jq 未返回内容, 已尝试无 jq 兜底})"
		return 1
	fi
	local res code="000" toolerr=""
	res=$(__engine_api PUT /api/subscriptions/$id "$body" 15 2>/dev/null)
	code=$(__engine_api_code)
	toolerr=$(__engine_api_err)
	# 4xx 业务原文透传, 不再误报「引擎无响应(jq: ...)」。
	if [ "$code" -ge 400 ] 2>/dev/null && [ -n "$res" ]; then
		echo "保存失败: $(__engine_err_text "$res")"
		return 1
	fi
	if [ -z "$res" ]; then
		echo "保存失败: 引擎 API 未返回响应体(HTTP $code)${toolerr:+(工具 stderr: $toolerr)}"
		return 1
	fi
	echo "已保存"
	return 0
}

# ============ show ============

show()
{
	Show __json_result__
}

# [CA下载通道] 页面是HTTPS(路由器Web UI), 直链 http://IP:18061/api/ca.pem 会被
# 浏览器当混合内容拦掉(现象: 点了一闪而过没下载)。改走同源的 show&TYPE=ca:
# CA 内容经本接口(base64)回传, 页面端 Blob 下载, 全程同源无混合内容。
# [diag] GEOIP 下载失败诊断: 不吞错误, 两种参数顺序都测, 连DNS/目录可写性一起回报
__show_diag()
{
	# [diag] 磁盘实况: 两个分区的余量 + 配置目录占用TOP(定位谁吃满了空间)
	local __r="" __u __sz __err __wr __dfc __dfi __du
	__dfc=$(df -h $DATADIR 2>/dev/null | tail -1 | tr -s ' ' | tr -d '"')
	__dfi=$(df -h $EXT_PLUGIN_INSTALL_DIR 2>/dev/null | tail -1 | tr -s ' ' | tr -d '"')
	# busybox sort 不支持 -rh, 用 KB 数值排序; tab 必须换空格——
	# 字面 tab 在 JSON 字符串里是控制字符, 严格解析器(页面 JSON.parse)会直接抛错
	__du=$(du -sk $EXT_PLUGIN_CONFIG_DIR/* 2>/dev/null | sort -rn | head -8 | tr '\t\n' ' ' | tr -s ' ' | tr -d '"')
	__wr=$([ -w "$DATADIR" ] && echo yes || echo no)
	__json_result__="{\"mark\":\"diag_ran\",\"df_config\":\"$__dfc\",\"df_install\":\"$__dfi\",\"du_config_top\":\"$__du\",\"writable\":\"$__wr\",\"datadir\":\"$DATADIR\"}"
	return 0
}

# [netdiag] 网络层诊断: 把 iptables 规则/设备表/scope/引擎日志/调度状态一次带回。
# 排查"设备勾选了却断网+无流量"类问题: 规则没匹配上 / scope only+空=全绕过 /
# 引擎隧道失败, 三种根因从此一个动作分辨。
__show_netdiag()
{
	local nat_rules fwd_rules devs scope logtail sched lanip
	nat_rules=$(iptables -t nat -S RUNHOOKS_REDIRECT 2>&1 | tr '\n' ';' | tr -d '"\\\r	')
	nat_rules="$nat_rules PREROUTING: $(iptables -t nat -S PREROUTING 2>&1 | grep RUNHOOKS | tr '\n' ',')"
	fwd_rules=$(iptables -S RUNHOOKS_QUIC 2>&1 | tr '\n' ';' | tr -d '"\\\r	')
	fwd_rules="$fwd_rules FORWARD: $(iptables -S FORWARD 2>&1 | grep RUNHOOKS | tr '\n' ',')"
	# [netdiag-json] devices/scope 是 JSON 文件原文, 含裸换行 —— 拼进 JSON 字符串后
	# 整个 /Action/call 响应就是非法 JSON(控制字符), 前端 JSON.parse 直接抛异常,
	# netdiag 面板整个打不开(2026-11-17 真机实测: requests 报 Invalid control
	# character at char 763)。nat/filter/logtail 都做了 tr, 这两个漏了。
	devs=$(cat $DEVICES_FILE 2>/dev/null | tr -d '"\\\n\r	' | head -c 400)
	scope=$(cat $SCOPE_FILE 2>/dev/null | tr -d '"\\\n\r	' | head -c 600)
	logtail=$(tail -12 $LOGDIR/runhooks.log 2>/dev/null | tr '\n' '|' | tr -d '"\\\r	')
	sched=$([ -f $SCHED_PIDFILE ] && echo "pid=$(cat $SCHED_PIDFILE 2>/dev/null) alive=$(kill -0 $(cat $SCHED_PIDFILE 2>/dev/null) 2>/dev/null && echo yes || echo no)" || echo none)
	lanip=$(ip route 2>/dev/null | awk '/src/ {for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
	__json_result__="{\"mark\":\"netdiag\",\"nat\":\"$nat_rules\",\"filter\":\"$fwd_rules\",\"devices\":\"$devs\",\"scope\":\"$scope\",\"logtail\":\"$logtail\",\"sched\":\"$sched\",\"lanip\":\"$lanip\",\"rules_flag\":\"$([ -f $RULES_FLAG ] && echo on || echo off)\"}"
	return 0
}

__show_ca()
{
	local caf="" f
	# [data-dir-fix] CA 随引擎 data-dir 落在数据分区(DATA_DIR), 旧版在配置分区(DATADIR)——
	# 两者都探, 兼容升级过渡期
	for f in $DATA_DIR/ca.pem $DATA_DIR/mitmproxy-ca-cert.pem $DATA_DIR/ca-cert.pem \
		$DATADIR/ca.pem $DATADIR/mitmproxy-ca-cert.pem $DATADIR/ca-cert.pem; do
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

# [subs] 订阅状态回传(页面"规则订阅"tab): 引擎 API 只绑 127.0.0.1, 经本机
# wget 取回; 用 jq -c 规整(引擎返回的是紧凑 JSON, 但 jq 走一遍可保证无裸
# 控制字符/格式问题, 顺带打上 engine:true 标志)。jq 不在时原样回传。
__show_subs()
{
	local raw
	raw=$(__engine_api GET /api/subscriptions "" 10 2>/dev/null)
	if [ -z "$raw" ]; then
		__json_result__='{"subs":[],"effective":{"domains":[],"dst_ips":[],"countries":[],"paths":[],"user_agents":[],"rules":0,"scripts":0},"engine":false}'
		return 0
	fi
	if command -v jq >/dev/null 2>&1; then
		__json_result__=$(echo "$raw" | jq -c '. + {engine:true}' 2>/dev/null)
	fi
	[ -z "$__json_result__" ] && __json_result__="$raw"
	return 0
}

# [subdetail] 单个订阅的解析明细(页面"明细"按钮): show&TYPE=subdetail&id=sub-xxx
__show_subdetail()
{
	[ -z "$id" ] && { __json_result__='{"error":"missing id"}'; return 0; }
	local raw
	raw=$(__engine_api GET /api/subscriptions/$id/parsed "" 10 2>/dev/null)
	if [ -z "$raw" ]; then
		__json_result__='{"error":"engine offline or sub not found"}'
		return 0
	fi
	if command -v jq >/dev/null 2>&1; then
		__json_result__=$(echo "$raw" | jq -c '.' 2>/dev/null)
	fi
	[ -z "$__json_result__" ] && __json_result__="$raw"
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
	{ [ -f $DATA_DIR/ca.pem ] || [ -f $DATA_DIR/mitmproxy-ca-cert.pem ] || [ -f $DATA_DIR/ca-cert.pem ] || [ -f $DATADIR/ca.pem ] || [ -f $DATADIR/mitmproxy-ca-cert.pem ] || [ -f $DATADIR/ca-cert.pem ]; } && caok=1
	local flows=0
	flows=$(__engine_api GET /api/flows "" 10 2>/dev/null | jq '.flows | length' 2>/dev/null)
	[ -z "$flows" ] && flows=0
	# [capture-pause] 引擎记录层是否暂停(WebUI 里点过"暂停"即为 1)。
	# 总开关开着但引擎暂停时页面要显示"已暂停"—— 否则用户看着"抓包中"却
	# 一条不记, 又以为是坏了(和"关了还在抓"是同一类状态不透明)。
	local enginePaused=0
	local __paused=$(__engine_paused)
	[ "$__paused" = "true" ] && enginePaused=1
	local proxyPort=$PROXY_PORT
	local apiPort=$API_PORT
	local transparentPort=$TRANSPARENT_PORT
	# [plugin-proxy] 页面按此决定 iframe 走同源代理路径还是直连(见 __web_bind_addr)
	local proxyReady=0
	[ "$(__web_bind_addr)" = "127.0.0.1" ] && proxyReady=1
	local geoLoaded=0
	[ -s $DATA_DIR/geoip.csv ] && geoLoaded=1
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

	json_append __json_result__ status:int capture:int enginePaused:int rulesOn:int autostart:int caok:int flows:int
	json_append __json_result__ proxyPort:int apiPort:int transparentPort:int geoLoaded:int inWindow:int proxyReady:int
	json_append __json_result__ devices:json scope:json schedule:json ports:str interfaces:str groups:json
	return 0
}
