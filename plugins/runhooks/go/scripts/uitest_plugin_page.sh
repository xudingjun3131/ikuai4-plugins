#!/bin/bash
# 插件页(iKuai iframe Vue 页)真实浏览器自测
set -u
AB="node /root/.hermes/tools/agent-browser-0.26.0-linux-x64/bin/agent-browser.js"
W=/tmp/rhpage-test
rm -rf $W; mkdir -p $W/shots

python3 /tmp/rh_mock_server.py > $W/mock.log 2>&1 &
MOCK=$!
sleep 1
echo "mock: $(curl -s -m 3 http://127.0.0.1:8899/plugins/runhooks/ | head -c 60)"

$AB open "http://127.0.0.1:8899/plugins/runhooks/" >/dev/null 2>&1
sleep 3
echo "== 控制台错误:"
$AB console 2>&1 | head -8
echo "== 页面标题/关键元素:"
echo "-- Vue 挂载: $($AB eval "!!document.querySelector('#app').__vue__" 2>&1)"
echo "-- 标题: $($AB eval "document.title" 2>&1)"
echo "-- 启动按钮(单按钮态): $($AB eval "Array.from(document.querySelectorAll('button')).map(b=>b.textContent.trim()).filter(t=>/启动|停止/.test(t))" 2>&1)"
echo "-- 设备tab: $($AB eval "Array.from(document.querySelectorAll('.rq-tab')).map(t=>t.textContent.trim())" 2>&1)"
$AB screenshot $W/shots/01-page.png

echo "== 设备列表(DHCP+ARP 合并, 3台含wan过滤)"
echo "-- 设备行: $($AB eval "document.querySelectorAll('.runhooks-device-item').length" 2>&1)"
echo "-- 含wan被过滤: $($AB eval "document.body.innerText.includes('10.10.10.6')" 2>&1)"
echo "-- 勾选一台: $($AB eval "(() => {const items=document.querySelectorAll('.runhooks-device-item input[type=checkbox]'); if(!items.length) return 'NO-CHECKBOX'; items[0].click(); return 'clicked';})()" 2>&1)"
sleep 1
echo "-- 已选标签云: $($AB eval "document.body.innerText.includes('test-phone')" 2>&1)"
$AB screenshot $W/shots/02-devices.png

echo "== 切到 规则订阅 tab"
$AB eval "(() => {const t=Array.from(document.querySelectorAll('.rq-tab')).find(x=>x.textContent.includes('规则订阅')); t.click(); return 'clicked';})()" >/dev/null 2>&1
sleep 2
echo "-- 订阅卡片: $($AB eval "document.body.innerText.includes('演示订阅')" 2>&1)"
echo "-- 生效总览: $($AB eval "document.body.innerText.includes('当前生效')" 2>&1)"
echo "-- 域名数: $($AB eval "(document.body.innerText.match(/解密域名\s*(\d+)/)||[])[1]" 2>&1)"
echo "-- 引擎运行提示(无'引擎未运行'): $($AB eval "!document.body.innerText.includes('引擎未运行')" 2>&1)"
$AB screenshot $W/shots/03-subs.png

echo "== 展开明细"
$AB eval "(() => {const b=Array.from(document.querySelectorAll('button')).find(x=>x.textContent.trim()==='明细'); if(b){b.click(); return 'clicked';} return 'NO-BUTTON';})()" >/dev/null 2>&1
sleep 1.5
echo "-- 明细渲染: $($AB eval "document.body.innerText.includes('解析明细')" 2>&1)"
echo "-- 规则行: $($AB eval "document.body.innerText.includes('DOMAIN-SUFFIX,demo.example.com,PROXY')" 2>&1)"
echo "-- 适配说明: $($AB eval "document.body.innerText.includes('DOMAIN-SET')" 2>&1)"
$AB screenshot $W/shots/04-detail.png

echo "== 其它 tab 回归"
for t in 网络范围 解密范围 记录范围; do
  $AB eval "(() => {const x=Array.from(document.querySelectorAll('.rq-tab')).find(e=>e.textContent.trim()==='$t'); if(x){x.click(); return 'ok';} return 'NO';})()" >/dev/null 2>&1
  sleep 1
  echo "-- $t: $($AB eval "document.querySelectorAll('.rq-tabpanel:not([style*=\"display: none\"])').length" 2>&1)"
done
$AB screenshot $W/shots/05-tabs.png
echo "== 最终控制台错误: $($AB console 2>&1 | grep -c '\[error\]')"
kill $MOCK 2>/dev/null; wait 2>/dev/null
echo "SHOTS:"; ls -la $W/shots/
echo DONE