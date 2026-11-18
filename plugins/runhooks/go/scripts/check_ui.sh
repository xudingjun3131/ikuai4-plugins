#!/bin/bash
# 验证内嵌 WebUI 含订阅面板(起引擎→拉 JS→grep 关键词→杀)
set -u
cd /opt/runhooks-engine
WORK=$(mktemp -d /tmp/rhui.XXXXXX)
./bin/runhooks -proxy-addr 127.0.0.1:18060 -api-addr 127.0.0.1:18061 -data-dir $WORK > $WORK/eng.log 2>&1 &
ENG=$!
for i in $(seq 1 30); do curl -s -o /dev/null http://127.0.0.1:18061/api/health && break; sleep 0.5; done
JS=$(curl -s http://127.0.0.1:18061/plugin_proxy/runhooks/ | grep -oE '/plugin_proxy/runhooks/assets/index-[^"]+\.js')
echo "JS path: $JS"
curl -s "http://127.0.0.1:18061$JS" -o $WORK/ui.js
for kw in 规则订阅 订阅地址 适配说明 更新于 QuanX 纯列表; do
  n=$(grep -c "$kw" $WORK/ui.js || true)
  echo "  [$kw] x$n"
done
echo "health: $(curl -s http://127.0.0.1:18061/api/health)"
echo "index.html base ok: $(curl -s http://127.0.0.1:18061/plugin_proxy/runhooks/ | grep -c 'plugin_proxy/runhooks')"
kill $ENG 2>/dev/null; wait $ENG 2>/dev/null
rm -rf $WORK
echo DONE