package proxy

import (
	"os"
	"testing"
)

// 本包单测全部从 127.0.0.1 起本地服务并造流; 生产默认会过滤本机源流量
// (self-probe-filter, 见 engine.go), 所以这里显式打开逃生开关, 让既有断言
// 继续有效。真实设备上该变量不会被设置, 过滤保持开启。
func TestMain(m *testing.M) {
	_ = os.Setenv("RUNHOOKS_RECORD_SELF_TRAFFIC", "1")
	os.Exit(m.Run())
}
