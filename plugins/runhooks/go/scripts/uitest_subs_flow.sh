#!/bin/bash
# 订阅面板 暂停/启用/删除 流程(选择器限定在面板内, 避免点到 header 的暂停)
set -u
AB="node /root/.hermes/tools/agent-browser-0.26.0-linux-x64/bin/agent-browser.js"
W=/tmp/rhui-test2
rm -rf $W; mkdir -p $W/subsrc $W/shots
cd /opt/runhooks-engine
cat > $W/subsrc/sub.conf <<'EOF'
[mitm]
hostname = %.uitest.example.com
DOMAIN-SUFFIX,uitest.example.com,PROXY
[rewrite]
^https?://api\.uitest\.example\.com/v1/user request-header X-Ui: 1
EOF
python3 -m http.server 23458 --bind 127.0.0.1 --directory $W/subsrc >/dev/null 2>&1 &
SRV=$!
./bin/runhooks -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $W/data > $W/e.log 2>&1 &
ENG=$!
for i in $(seq 1 40); do curl -s -o /dev/null -m 2 http://127.0.0.1:18061/api/health 2>/dev/null && break; sleep 0.5; done
$AB open "http://127.0.0.1:18061/plugin_proxy/runhooks/" >/dev/null 2>&1
sleep 3
$AB click 'button[data-testid="nav-subs"]' >/dev/null 2>&1
sleep 1.5
$AB fill '#sub-url' "http://127.0.0.1:23458/sub.conf" >/dev/null 2>&1
$AB eval "Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='添加').click()" >/dev/null 2>&1
sleep 3
echo "== 添加后: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已启用')" 2>&1)"

# 面板内找按钮(避开 header 的同名按钮)
panel_btn() {
  $AB eval "(() => {const p=document.querySelector('[data-testid=subs-panel]'); const b=Array.from(p.querySelectorAll('button')).find(x=>x.textContent.trim()==='$1'); if(!b) return 'NO-BUTTON'; b.click(); return 'clicked';})()" 2>&1
}
echo "== 点[暂停]: $(panel_btn 暂停)"
sleep 2
echo "-- 面板显示已暂停: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已暂停')" 2>&1)"
echo "-- 生效域名归零: $($AB eval "(document.querySelector('[data-testid=subs-panel]').innerText.match(/解密域名\s*(\d+)/)||[])[1]" 2>&1)"
$AB screenshot $W/shots/paused.png
echo "== 点[启用]: $(panel_btn 启用)"
sleep 2
echo "-- 面板显示已启用: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已启用')" 2>&1)"
echo "-- 生效域名恢复: $($AB eval "(document.querySelector('[data-testid=subs-panel]').innerText.match(/解密域名\s*(\d+)/)||[])[1]" 2>&1)"
echo "== 点[更新]: $(panel_btn 更新)"
sleep 3
echo "-- 更新后仍在: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('已启用')" 2>&1)"
echo "== 点[删除]: $(panel_btn 删除)"
sleep 2
echo "-- 回到空态: $($AB eval "document.querySelector('[data-testid=subs-panel]').innerText.includes('还没有订阅')" 2>&1)"
$AB screenshot $W/shots/deleted.png
echo "== console 错误: $($AB console 2>&1 | grep -v 'WS error' | grep -c error)"
kill $ENG $SRV 2>/dev/null; wait 2>/dev/null
echo DONE