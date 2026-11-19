package app

import (
	"os"
	"testing"
)

// 本包单测从 127.0.0.1 起本地服务并造流; 生产默认会过滤本机源流量
// (self-probe-filter), 这里显式打开逃生开关让既有断言继续有效。
func TestMain(m *testing.M) {
	_ = os.Setenv("RUNHOOKS_RECORD_SELF_TRAFFIC", "1")
	os.Exit(m.Run())
}
