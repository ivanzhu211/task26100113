package attribution

import (
	"fmt"
	"math"
	"runtime"
	"testing"
	"time"
)

// 规模：20 万触点 + 20 万转化 + 10 万渠道活动组合，全部属于一个客户，
// 每笔转化覆盖超过 10 万触点。验证总耗时 < 10s、峰值内存 < 256MiB，
// 并在该规模下校验金额精度（小范围与参考实现对比 + 全量收入守恒）。
func TestHighDensitySingleCustomer(t *testing.T) {
	const (
		nTP     = 200_000
		nCV     = 200_000
		nCombo  = 100_000
		maxCent = 1_000_000 // 100 万元分 = 1 万元
	)

	tps := make([]Touchpoint, 0, nTP)
	for i := 0; i < nTP; i++ {
		tps = append(tps, Touchpoint{
			CustomerID: "whale",
			Time:       int64(i), // 0 .. 199999，全部落在任意转化的 30 天窗口内
			Channel:    fmt.Sprintf("ch%04d", i%nCombo/100),
			Campaign:   fmt.Sprintf("camp%03d", i%100),
		})
	}
	cvs := make([]Conversion, 0, nCV)
	for i := 0; i < nCV; i++ {
		cvs = append(cvs, Conversion{
			CustomerID: "whale",
			Time:       int64(200_000 + i), // 200000 .. 399999，每笔覆盖全部 20 万触点
			Revenue:    int64((i*7919)%maxCent + 1),
		})
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	start := time.Now()

	eng := New()
	eng.Load(tps, cvs)
	sum := eng.Summarize(0, 1<<62)

	elapsed := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	liveMiB := float64(memAfter.Alloc) / (1 << 20)
	totalAllocMiB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / (1 << 20)
	t.Logf("load+query elapsed: %v", elapsed)
	t.Logf("live heap after query: %.1f MiB, total allocated: %.1f MiB", liveMiB, totalAllocMiB)

	if elapsed > 10*time.Second {
		t.Errorf("elapsed %v exceeds 10s", elapsed)
	}
	if liveMiB > 256 {
		t.Errorf("live heap %.1f MiB exceeds 256 MiB", liveMiB)
	}

	// 收入守恒：已归因 + 未归因 = 范围内收入总额（容差 0.001 元 = 0.1 分）
	var total float64
	for _, cv := range cvs {
		total += float64(cv.Revenue)
	}
	var got float64
	for _, g := range sum.Groups {
		got += g.Amount
	}
	got += sum.Unattributed
	if diff := math.Abs(got - total); diff > 0.1 {
		t.Errorf("conservation at scale: got %v, want %v (diff %v 分)", got, total, diff)
	}
	if sum.Unattributed != 0 {
		t.Errorf("expected no unattributed, got %v", sum.Unattributed)
	}
	if len(sum.Groups) != nCombo {
		t.Errorf("expected %d groups, got %d", nCombo, len(sum.Groups))
	}

	// 精度抽查：取 10 笔转化的窄范围，与逐笔展开的参考实现对比（容差 0.0001 元 = 0.01 分）
	rStart, rEnd := int64(250_000), int64(250_010)
	sub := eng.Summarize(rStart, rEnd)
	var subCVs []Conversion
	for _, cv := range cvs {
		if cv.Time >= rStart && cv.Time < rEnd {
			subCVs = append(subCVs, cv)
		}
	}
	wantGroups, wantUnattr := referenceSummarize(tps, subCVs, rStart, rEnd)
	gotMap := map[[2]string]float64{}
	for _, g := range sub.Groups {
		gotMap[[2]string{g.Channel, g.Campaign}] = g.Amount
	}
	var maxDiff float64
	for key, want := range wantGroups {
		if diff := math.Abs(gotMap[key] - want); diff > maxDiff {
			maxDiff = diff
		}
	}
	t.Logf("sub-range precision check: %d groups, max diff %.3e 分", len(wantGroups), maxDiff)
	if maxDiff > 0.01 {
		t.Errorf("per-group diff %v 分 exceeds 0.0001 元 (0.01 分)", maxDiff)
	}
	if math.Abs(sub.Unattributed-wantUnattr) > 0.01 {
		t.Errorf("unattributed diff too large: got %v want %v", sub.Unattributed, wantUnattr)
	}
}
