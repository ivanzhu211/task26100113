package attribution

import (
	"fmt"
	"math"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// 规模：20 万触点、20 万转化、10 万个渠道+活动组合，全部属于一个客户。
const (
	perfTouches  = 200_000
	perfConvs    = 200_000
	perfChannels = 1_000
	perfCampaign = 100 // 1000*100 = 10 万组合
	perfBase     = int64(1_700_000_000)
)

// genPerfData 生成单客户高密度数据：触点均匀分布在约 27.8 天内，
// 转化均匀分布在后半段，保证每笔转化的 30 天窗口覆盖至少 10 万个触点。
func genPerfData() ([]Touchpoint, []Conversion) {
	touches := make([]Touchpoint, perfTouches)
	for i := range touches {
		combo := i % (perfChannels * perfCampaign)
		touches[i] = Touchpoint{
			CustomerID: "whale",
			Time:       perfBase + int64(i)*12, // 200000*12s ≈ 27.8 天
			Channel:    fmt.Sprintf("ch%04d", combo/perfCampaign),
			Campaign:   fmt.Sprintf("cp%02d", combo%perfCampaign),
		}
	}
	convs := make([]Conversion, perfConvs)
	for j := range convs {
		convs[j] = Conversion{
			CustomerID:   "whale",
			Time:         perfBase + 1_200_000 + int64(j)*6, // 窗口左端越过全部触点起点
			RevenueCents: int64(j*7919)%1_000_000 + 1,
		}
	}
	return touches, convs
}

// TestPerfSingleCustomerDense 在规定的最大规模下验证：
// 载入 + 一次全范围汇总总耗时 < 10s，峰值内存 < 256 MiB，
// 且金额精度（收入守恒 + 抽样逐转化比对参考实现）仍满足要求。
func TestPerfSingleCustomerDense(t *testing.T) {
	touches, convs := genPerfData()

	// 确认规模假设：首笔转化窗口覆盖 >= 10 万触点。
	first := convs[0].Time
	var covered int
	for _, tp := range touches {
		if d := first - tp.Time; d >= 0 && d <= WindowSeconds {
			covered++
		}
	}
	if covered < 100_000 {
		t.Fatalf("规模假设不成立：首笔转化窗口仅覆盖 %d 触点", covered)
	}
	t.Logf("首笔转化窗口覆盖触点 %d 个", covered)

	debug.SetGCPercent(-1) // 关闭 GC，HeapAlloc 增量即分配总量上界
	defer debug.SetGCPercent(100)
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)

	start := time.Now()
	a := New()
	a.Load(touches, convs)
	rep := a.Query(0, math.MaxInt64/2)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&m1)
	peakMiB := float64(m1.HeapAlloc-m0.HeapAlloc) / (1 << 20)
	t.Logf("载入+一次全范围汇总耗时 %v，堆内存增量 %.1f MiB", elapsed, peakMiB)
	if elapsed > 10*time.Second {
		t.Errorf("耗时 %v 超过 10s 上限", elapsed)
	}
	if peakMiB > 256 {
		t.Errorf("内存 %.1f MiB 超过 256 MiB 上限", peakMiB)
	}
	if len(rep.Groups) != perfChannels*perfCampaign {
		t.Errorf("组合数 = %d, want %d", len(rep.Groups), perfChannels*perfCampaign)
	}

	// 收入守恒：所有组金额 + 未归因 ≈ 范围内转化收入总额。
	var totalRev float64
	for _, cv := range convs {
		totalRev += float64(cv.RevenueCents)
	}
	if d := math.Abs(rep.Total() - totalRev); d > totalTol {
		t.Errorf("收入不守恒：报告总额 %.6f, 转化收入 %.6f, diff %.6f > %v", rep.Total(), totalRev, d, totalTol)
	}
	if rep.Unattributed != 0 {
		t.Errorf("本数据每笔转化都有合格触点，未归因应为 0, got %v", rep.Unattributed)
	}

	// 精度抽查：取 20 笔不同转化，分别按秒级范围查询并与参考实现比对。
	rng := []int{0, 1, 9999, 50000, 99999, 150000, 199999, 12345, 54321, 77777,
		3, 22222, 33333, 44444, 66666, 88888, 111111, 133333, 155555, 188888}
	for _, j := range rng {
		T := convs[j].Time
		got := a.Query(T, T+1)
		want := bruteForce(touches, convs, T, T+1)
		checkReport(t, got, want, float64(convs[j].RevenueCents))
	}
}
