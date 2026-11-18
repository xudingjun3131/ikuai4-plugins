package netcond_test

import (
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/netcond"
)

func Test_Latency_delay(t *testing.T) {
	c := netcond.New()
	c.Set(netcond.Config{Enabled: true, Latency: 40 * time.Millisecond})
	start := time.Now()
	c.SleepLatency()
	elapsed := time.Since(start)
	if elapsed < 35*time.Millisecond {
		t.Fatalf("elapsed=%v want >=35ms", elapsed)
	}
}

func Test_Disabled_no_sleep(t *testing.T) {
	c := netcond.New()
	c.Set(netcond.Config{Enabled: false, Latency: time.Second})
	start := time.Now()
	c.SleepLatency()
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("slept while disabled")
	}
}
