#!/bin/bash
# 引擎 WebUI 订阅面板真实浏览器自测(chromium headless)
set -u
AB="node /root/.hermes/tools/agent-browser-0.26.0-linux-x64/bin/agent-browser.js"
W=/tmp/rhui-test
rm -rf $W; mkdir -p $W/subsrc $W/shots
cd /opt/runhooks-engine

# ---- 订阅源 ----
cat > $W/subsrc/sub.conf <<'EOF'
[mitm]
hostname = %.uitest.example.com

DOMAIN-SUFFIX,uitest.example.com,PROXY
DOMAIN-SUFFIX,blocked.example.com,REJECT

[rewrite]
^https?://api\.uitest\.example\.com/v1/user url script-response-body unlock.js
^https?://api\.uitest\.example\.com/v1/user request-header X-Ui: 1
^https?://api\.uitest\.example\.com/echo url echo-response pong
^https?://ads\.uitest\.example\.com/ url reject
EOF
cat > $W/subsrc/unlock.js <<'EOF'
let obj = JSON.parse($response.body);
obj.ui = "unlocked";
$done({body: JSON.stringify(obj)});
EOF
python3 -m http.server 23457 --bind 127.0.0.1 --directory $W/subsrc >/dev/null 2>&1 &
SRV=$!

# ---- 引擎 ----
./bin/runhooks -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $W/data > $W/engine.log 2>&1 &
ENG=$!
for i in $(seq 1 40); do curl -s -o /dev/null -m 2 http://127.0.0.1:18061/api/health 2>/dev/null && break; sleep 0.5; done
echo "engine health: $(curl -s -m 3 http://127.0.0.1:18061/api/health)"

# ---- 浏览器 ----
$AB open "http://127.0.0.1:18061/plugin_proxy/runhooks/" >/dev/null 2>&1
sleep 3
echo "== 控制台错误(应为空):"
$AB console 2>&1 | head -10
echo "== 初始快照(导航栏):"
$AB snapshot -i 2>&1 | grep -E 'nav-|button' | head -10
$AB screenshot $W/shots/01-capture.png

echo "== 切到订阅 tab"
$AB click 'button[data-testid="nav-subs"]' >/dev/null 2>&1 || $AB click '@nav-subs' >/dev/null 2>&1
sleep 1.5
$AB screenshot $W/shots/02-subs-empty.png
echo "-- 面板存在: $($AB eval "!!document.querySelector('[data-testid=subs-panel]')" 2>&1)"
echo "-- 空态文案: $($AB eval "document.querySelector('[data-testid=subs-panel]')?.innerText.includes('还没有订阅')" 2>&1)"
echo "-- 添加表单: $($AB eval "!!document.querySelector('#sub-url')" 2>&1)"

echo "== 填表单并添加订阅"
$AB fill '#sub-url' "http://127.0.0.1:23457/sub.conf" >/dev/null 2>&1
$AB fill '#sub-name' "UI自测订阅" >/dev/null 2>&1
$AB screenshot $W/shots/03-subs-form.png
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='添加').click()" >/dev/null 2>&1
sleep 3
$AB screenshot $W/shots/04-subs-added.png
echo "-- 订阅卡片: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('UI自测订阅')" 2>&1)"
echo "-- 生效总览: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('当前生效')" 2>&1)"
echo "-- 域名数: $($AB eval "(document.querySelector('[data-testid=subs-panel]').innerText.match(/解密域名\s*(\d+)/)||[])[1]" 2>&1)"

echo "== 点明细"
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='明细').click()" >/dev/null 2>&1
sleep 1.5
$AB screenshot $W/shots/05-subs-detail.png
echo "-- 明细内容: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('解析明细')" 2>&1)"
echo "-- mitm 主机: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('%.uitest.example.com')" 2>&1)"

echo "== 暂停再启用"
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='暂停').click()" >/dev/null 2>&1
sleep 2
echo "-- 暂停态: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已暂停')" 2>&1)"
$AB screenshot $W/shots/06-subs-paused.png
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='启用').click()" >/dev/null 2>&1
sleep 2
echo "-- 启用态: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已启用')" 2>&1)"

echo "== 删除订阅"
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='删除').click()" >/dev/null 2>&1
sleep 2
echo "-- 回到空态: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('还没有订阅')" 2>&1)"
$AB screenshot $W/shots/07-subs-deleted.png

echo "== 回归: 其它 tab 仍正常"
for t in nav-rules nav-rest nav-explorer nav-scripts nav-capture; do
  $AB click "button[data-testid=$t]" >/dev/null 2>&1; sleep 0.8
  echo "-- $t: $($AB eval "document.body.innerText.length>50" 2>&1)"
done
$AB screenshot $W/shots/08-regression.png
echo "== 最终控制台错误:"
$AB console 2>&1 | head -10

kill $ENG $SRV 2>/dev/null; wait 2>/dev/null
echo "SHOTS:"; ls -la $W/shots/
echo DONE
