#!/bin/bash
set -u
AB="node /root/.hermes/tools/agent-browser-0.26.0-linux-x64/bin/agent-browser.js"
$AB close --all >/dev/null 2>&1
W=/tmp/rhclean
rm -rf $W; mkdir -p $W/subsrc
cd /opt/runhooks-engine
cat > $W/subsrc/sub.conf <<'EOF'
[mitm]
hostname = %.clean.example.com
DOMAIN-SUFFIX,clean.example.com,PROXY
EOF
python3 -m http.server 23459 --bind 127.0.0.1 --directory $W/subsrc >/dev/null 2>&1 &
SRV=$!
./bin/runhooks -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $W/data > $W/e.log 2>&1 &
ENG=$!
for i in $(seq 1 40); do curl -s -o /dev/null -m 2 http://127.0.0.1:18061/api/health 2>/dev/null && break; sleep 0.5; done
$AB open "http://127.0.0.1:18061/plugin_proxy/runhooks/" >/dev/null 2>&1
sleep 3
echo "== 干净环境 console(引擎 UI):"; $AB console 2>&1 | sort | uniq -c | sort -rn | head -6
echo "== page errors:"; $AB errors 2>&1 | head -6
$AB click 'button[data-testid="nav-subs"]' >/dev/null 2>&1
sleep 2
echo "== 订阅面板渲染: $($AB eval "!!document.querySelector('[data-testid=subs-panel]') && document.querySelector('[data-testid=subs-panel]').innerText.includes('还没有订阅')" 2>&1)"
echo "== 切回抓包: $($AB eval "(() => {document.querySelector('button[data-testid=nav-capture]').click(); return document.body.innerText.includes('TRAFFIC');})()" 2>&1)"
echo "== 最终 console:"; $AB console 2>&1 | sort | uniq -c | sort -rn | head -4
kill $ENG $SRV 2>/dev/null; wait 2>/dev/null
$AB close --all >/dev/null 2>&1
echo DONE