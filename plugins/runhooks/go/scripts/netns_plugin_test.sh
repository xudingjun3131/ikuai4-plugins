#!/bin/bash
# runhooks 插件 service.sh netns 隔离测试(宿主机零规则, 一等约束)
# 模拟 ikcommon 分发: source service.sh → local $@ → 调动作函数。
set -u
PLUG_SRC=/opt/ikuai4-plugins/plugins/runhooks
ROOT=/tmp/rhtest
rm -rf $ROOT
mkdir -p $ROOT/plugins_installed $ROOT/configs/runhooks $ROOT/logs/runhooks $ROOT/subsrc
cp -a $PLUG_SRC $ROOT/plugins_installed/runhooks

cat > $ROOT/subsrc/sub.conf <<'EOF'
[mitm]
hostname = %.nstest.example.com

DOMAIN-SUFFIX,nstest.example.com,PROXY
DOMAIN-SUFFIX,blocked.example.com,REJECT

[rewrite]
^https?://api\.nstest\.example\.com/v1/user request-header X-Ns: 1
^https?://ads\.nstest\.example\.com/ url reject
EOF

unshare -n bash <<'EONS'
set -u
ip link set lo up
ROOT=/tmp/rhtest
export EXT_PLUGIN_INSTALL_DIR=$ROOT/plugins_installed
export EXT_PLUGIN_CONFIG_DIR=$ROOT/configs
export EXT_PLUGIN_LOG_DIR=$ROOT/logs
export EXT_PLUGIN_IPK_DIR=$ROOT/ipk
SVC=$EXT_PLUGIN_INSTALL_DIR/runhooks/script/service.sh

# 模拟 ikcommon: source + local key=value + 调动作; show 动作按 TYPE 分发
# __show_$TYPE 后回显 __json_result__(与固件 Show() 同语义)
run_action() {
    local action="$1"; shift
    RH_ACTION="$action" RH_ARGS="$*" bash -c '
        Show() {
            local __t="${TYPE//,/ }" __x __fn
            for __x in $__t; do
                __fn="__show_$__x"
                declare -F "$__fn" >/dev/null 2>&1 && "$__fn"
            done
            eval echo -n \"\$$1\"
        }
        . "$0" || exit 99
        __run() { local $RH_ARGS; $RH_ACTION; }
        __run
    ' "$SVC"
}

echo "== 1) 造一台假设备"
cat > $ROOT/configs/runhooks/devices.json <<'DEV'
[{"ip":"192.168.9.100","mac":"aa:bb:cc:dd:ee:ff","name":"ns-test-device"}]
DEV

echo "== 2) 启动引擎(netns 内)"
run_action start; echo "start rc=$?"
for i in $(seq 1 40); do curl -s -o /dev/null -m 2 http://127.0.0.1:18061/api/health 2>/dev/null && break; sleep 0.5; done
echo "health: $(curl -s -m 3 http://127.0.0.1:18061/api/health 2>/dev/null)"
echo "ports: $(ss -tln | grep -cE '1806[012]')/3 监听中"
echo "pidfile: $(cat $ROOT/configs/runhooks/runhooks.pid 2>/dev/null)"

echo "== 3) 开启抓包 → iptables 规则"
run_action set_capture capture=true
echo "--- nat RUNHOOKS_REDIRECT:"; iptables -t nat -S RUNHOOKS_REDIRECT 2>&1
echo "--- filter RUNHOOKS_QUIC:"; iptables -S RUNHOOKS_QUIC 2>&1 | head -4
echo "REDIRECT 条数: $(iptables -t nat -S RUNHOOKS_REDIRECT 2>/dev/null | grep -c REDIRECT)"

echo "== 4) 订阅(经 service.sh 中转 → 引擎 API)"
python3 -m http.server 23555 --bind 127.0.0.1 --directory $ROOT/subsrc >/dev/null 2>&1 &
SRV=$!
sleep 1
URL_B64=$(printf 'http://127.0.0.1:23555/sub.conf' | base64 -w0)
NAME_B64=$(printf 'ns测试订阅' | base64 -w0)
run_action sub_add url_b64=$URL_B64 name_b64=$NAME_B64 format=auto interval_min=0
SUBID=$(ls $ROOT/.runhooks_data/subscriptions/*.parsed.json 2>/dev/null | head -1 | xargs -r basename | sed 's/.parsed.json//')
echo "SUBID=$SUBID"
echo "rules.sub.json:"; jq -c '.rules[] | {id, type, host: .match.host, path: .match.path}' $ROOT/.runhooks_data/rules.sub.json 2>/dev/null
echo "scope.sub.json:"; cat $ROOT/.runhooks_data/scope.sub.json 2>/dev/null
echo "scripts: $(ls $ROOT/.runhooks_data/scripts/ 2>/dev/null | tr '\n' ' ')"
echo "生效域名(引擎视角): $(curl -s -m 3 http://127.0.0.1:18061/api/subscriptions 2>/dev/null | jq -c '.effective.domains' 2>/dev/null)"

echo "== 5) show TYPE=subs(页面数据通道)"
run_action show TYPE=subs | head -c 600; echo

echo "== 6) 关闭抓包 → 规则撤净"
run_action set_capture capture=false
echo "nat REDIRECT 残留: $(iptables -t nat -S RUNHOOKS_REDIRECT 2>/dev/null | grep -c REDIRECT)"
echo "PREROUTING 残留: $(iptables -t nat -S PREROUTING 2>/dev/null | grep -c RUNHOOKS)"

echo "== 7) 删除订阅 + 停引擎"
[ -n "$SUBID" ] && run_action sub_del id=$SUBID
echo "删除后 rules: $(jq '.rules | length' $ROOT/.runhooks_data/rules.sub.json 2>/dev/null)"
run_action stop
sleep 1
echo "停止后端口: $(ss -tln | grep -cE '1806[012]')/3"
echo "端口文件: $([ -f /tmp/iktmp/plugin_web/runhooks ] && echo 残留 || echo 已清)"

echo "== 8) netns 内最终核查(应无自定义链)"
echo "nat: $(iptables -t nat -S 2>/dev/null | grep -c RUNHOOKS)  filter: $(iptables -S 2>/dev/null | grep -c RUNHOOKS)"
kill $SRV 2>/dev/null
echo NS-DONE
EONS
echo "== 宿主机核查(只读, 应全为 0)"
echo "nat: $(iptables -t nat -S 2>/dev/null | grep -c RUNHOOKS)  filter: $(iptables -S 2>/dev/null | grep -c RUNHOOKS)  ports: $(ss -tln | grep -cE '1806[012]')"
rm -rf $ROOT
echo HOST-CLEAN