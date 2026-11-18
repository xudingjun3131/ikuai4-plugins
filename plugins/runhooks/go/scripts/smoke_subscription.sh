#!/bin/bash
# runhooks 订阅链路端到端冒烟(本机回环, 不碰任何网络配置)
set -u
cd /opt/runhooks-engine
WORK=$(mktemp -d /tmp/rhsmoke.XXXXXX)
BIN=$PWD/bin/runhooks
mkdir -p $WORK/src $WORK/data

# ---- 本地订阅源(QuanX conf + 引用的 JS 脚本) ----
cat > $WORK/src/sub.conf <<'EOF'
[mitm]
hostname = %.smoke.example.com

DOMAIN-SUFFIX,smoke.example.com,PROXY
DOMAIN-SUFFIX,blocked.example.com,REJECT
IP-CIDR,203.0.113.0/24,DIRECT

[rewrite]
^https?://api\.smoke\.example\.com/v1/user url script-response-body unlock.js
^https?://api\.smoke\.example\.com/v1/user request-header X-Smoke: 1
^https?://api\.smoke\.example\.com/echo url echo-response pong
^https?://ads\.smoke\.example\.com/ url reject
EOF
cat > $WORK/src/unlock.js <<'EOF'
let obj = JSON.parse($response.body);
obj.smoke = "unlocked";
$done({body: JSON.stringify(obj)});
EOF
PORT=23456
python3 -m http.server $PORT --bind 127.0.0.1 --directory $WORK/src >/dev/null 2>&1 &
SRV=$!
for i in $(seq 1 20); do curl -s -o /dev/null "http://127.0.0.1:$PORT/sub.conf" && break; sleep 0.3; done
echo "== 订阅源 http://127.0.0.1:$PORT/sub.conf ($(curl -s http://127.0.0.1:$PORT/sub.conf | head -1))"

# ---- 起引擎(仅回环) ----
$BIN -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $WORK/data > $WORK/engine.log 2>&1 &
ENG=$!
for i in $(seq 1 30); do curl -s -o /dev/null http://127.0.0.1:18061/api/health && break; sleep 0.5; done
echo "== 引擎 pid=$ENG health=$(curl -s http://127.0.0.1:18061/api/health)"

echo "== 1) 新增订阅"
curl -s -X POST http://127.0.0.1:18061/api/subscriptions -d "{\"url\":\"http://127.0.0.1:$PORT/sub.conf\",\"name\":\"冒烟测试订阅\",\"format\":\"auto\",\"interval_min\":0}" | head -c 400; echo
SUBID=$(curl -s http://127.0.0.1:18061/api/subscriptions | python3 -c 'import sys,json; print(json.load(sys.stdin)["subs"][0]["id"])')
echo "== SUBID=$SUBID"

echo "== 2) 订阅层文件落地"
echo "-- scope.sub.json:"; cat $WORK/data/scope.sub.json
echo "-- rules.sub.json(类型/主机):"; python3 -c "
import json
d=json.load(open('$WORK/data/rules.sub.json'))
for r in d['rules']: print('  ', r['id'], r['type'], r['match'].get('host',''), r['match'].get('path',''))
"
echo "-- scripts:"; ls $WORK/data/scripts/ 2>/dev/null
echo "-- 转换后的脚本:"; cat $WORK/data/scripts/sub-$SUBID-1.js 2>/dev/null

echo "== 3) 生效范围(API)"
curl -s http://127.0.0.1:18061/api/subscriptions | python3 -c "
import sys,json
d=json.load(sys.stdin)
e=d['effective']
print('domains:', e['domains']); print('dst_ips:', e['dst_ips']); print('rules:', e['rules'], 'scripts:', e['scripts'])
s=d['subs'][0]
print('sub counts: rule=%d host=%d ip=%d rewrite=%d script=%d skipped=%d err=%r' % (s['rule_count'],s['host_count'],s['ip_count'],s['rewrite_count'],s['script_count'],s['skipped_count'],s.get('last_error','')))
"

echo "== 4) 暂停订阅 → 订阅层清空"
curl -s -X PUT http://127.0.0.1:18061/api/subscriptions/$SUBID -d '{"enabled":false}' >/dev/null
echo "rules.sub.json rules: $(python3 -c "import json;print(len(json.load(open('$WORK/data/rules.sub.json'))['rules']))")"
echo "scope.sub.json domains: $(python3 -c "import json;print(json.load(open('$WORK/data/scope.sub.json'))['domains'])")"

echo "== 5) 重新启用 + 立即更新"
curl -s -X PUT http://127.0.0.1:18061/api/subscriptions/$SUBID -d '{"enabled":true}' >/dev/null
curl -s -X POST http://127.0.0.1:18061/api/subscriptions/$SUBID/update >/dev/null
echo "rules: $(python3 -c "import json;print(len(json.load(open('$WORK/data/rules.sub.json'))['rules']))") domains: $(python3 -c "import json;print(json.load(open('$WORK/data/scope.sub.json'))['domains'])")"

echo "== 6) 删除订阅"
curl -s -X DELETE http://127.0.0.1:18061/api/subscriptions/$SUBID >/dev/null
echo "subs: $(curl -s http://127.0.0.1:18061/api/subscriptions | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["subs"]))')"

echo "== 7) 重新添加 + 引擎重启后自愈(订阅层文件不丢, 重启自动重放)"
curl -s -X POST http://127.0.0.1:18061/api/subscriptions -d "{\"url\":\"http://127.0.0.1:$PORT/sub.conf\",\"name\":\"重启自愈\",\"format\":\"auto\",\"interval_min\":0}" >/dev/null
kill $ENG; wait $ENG 2>/dev/null
$BIN -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $WORK/data >> $WORK/engine.log 2>&1 &
ENG=$!
for i in $(seq 1 30); do curl -s -o /dev/null http://127.0.0.1:18061/api/health && break; sleep 0.5; done
curl -s http://127.0.0.1:18061/api/subscriptions | python3 -c "import sys,json; d=json.load(sys.stdin); print('subs after restart:', len(d['subs']), 'effective domains:', d['effective']['domains'], 'rules:', d['effective']['rules'])"
echo "scope.sub.json domains after restart: $(python3 -c "import json;print(json.load(open('$WORK/data/scope.sub.json'))['domains'])")"

kill $ENG $SRV 2>/dev/null; wait 2>/dev/null
echo "== DONE (work=$WORK)"