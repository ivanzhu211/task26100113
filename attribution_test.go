package attribution

import (
	"math"
	"math/rand"
	"testing"
)

// referenceSummarize 按业务规则直接逐笔展开的参考实现。
func referenceSummarize(tps []Touchpoint, cvs []Conversion, start, end int64) (map[[2]string]float64, float64) {
	groups := map[[2]string]float64{}
	var unattr float64
	for _, cv := range cvs {
		if cv.Time < start || cv.Time >= end {
			continue
		}
		var idx []int
		var wsum float64
		for i, tp := range tps {
			if tp.CustomerID != cv.CustomerID {
				continue
			}
			if tp.Time < cv.Time-WindowSeconds || tp.Time > cv.Time {
				continue
			}
			idx = append(idx, i)
			wsum += math.Exp2(-float64(cv.Time-tp.Time) / HalfLifeSeconds)
		}
		if wsum == 0 {
			unattr += float64(cv.Revenue)
			continue
		}
		for _, i := range idx {
			tp := tps[i]
			w := math.Exp2(-float64(cv.Time-tp.Time) / HalfLifeSeconds)
			groups[[2]string{tp.Channel, tp.Campaign}] += float64(cv.Revenue) * w / wsum
		}
	}
	return groups, unattr
}

func shuffleEvents(tps []Touchpoint, cvs []Conversion, seed int64) ([]Touchpoint, []Conversion) {
	r := rand.New(rand.NewSource(seed))
	r.Shuffle(len(tps), func(i, j int) { tps[i], tps[j] = tps[j], tps[i] })
	r.Shuffle(len(cvs), func(i, j int) { cvs[i], cvs[j] = cvs[j], cvs[i] })
	return tps, cvs
}

// checkSummary 校验引擎结果与参考实现一致（容差远严于验收的 0.0001 元 = 0.01 分），
// 并校验收入守恒：已归因 + 未归因 = 范围内转化收入总额。
func checkSummary(t *testing.T, tps []Touchpoint, cvs []Conversion, start, end int64) {
	t.Helper()
	eng := New()
	eng.Load(tps, cvs)
	got := eng.Summarize(start, end)

	wantGroups, wantUnattr := referenceSummarize(tps, cvs, start, end)

	gotMap := map[[2]string]float64{}
	for _, g := range got.Groups {
		gotMap[[2]string{g.Channel, g.Campaign}] = g.Amount
	}
	const tol = 1e-6 // 分
	for key, want := range wantGroups {
		if diff := math.Abs(gotMap[key] - want); diff > tol {
			t.Errorf("group %v: got %v, want %v (diff %v 分)", key, gotMap[key], want, diff)
		}
	}
	for key, got := range gotMap {
		if _, ok := wantGroups[key]; !ok {
			t.Errorf("unexpected group %v with amount %v", key, got)
		}
	}
	if diff := math.Abs(got.Unattributed - wantUnattr); diff > tol {
		t.Errorf("unattributed: got %v, want %v", got.Unattributed, wantUnattr)
	}

	// 收入守恒
	var total float64
	for _, cv := range cvs {
		if cv.Time >= start && cv.Time < end {
			total += float64(cv.Revenue)
		}
	}
	var sum float64
	for _, g := range got.Groups {
		sum += g.Amount
	}
	sum += got.Unattributed
	if diff := math.Abs(sum - total); diff > 0.1 { // 验收：0.001 元 = 0.1 分
		t.Errorf("conservation: attributed+unattributed=%v, total revenue=%v", sum, total)
	}
	for _, g := range got.Groups {
		if math.IsNaN(g.Amount) || math.IsInf(g.Amount, 0) {
			t.Errorf("non-finite amount for %v", g.Channel)
		}
	}
}

// 场景一：乱序输入下，多次转化共享同一批触点；同一活动多次触达分别贡献。
func TestSharedTouchpointsAcrossConversions(t *testing.T) {
	tps := []Touchpoint{
		{CustomerID: "u1", Time: 1000, Channel: "sem", Campaign: "brand"},
		{CustomerID: "u1", Time: 2000, Channel: "social", Campaign: "promo"},
		{CustomerID: "u1", Time: 3000, Channel: "sem", Campaign: "brand"}, // 同活动再次触达
		{CustomerID: "u2", Time: 1500, Channel: "edm", Campaign: "news"},
	}
	cvs := []Conversion{
		{CustomerID: "u1", Time: 4000, Revenue: 10000},
		{CustomerID: "u1", Time: 5000, Revenue: 20000}, // 与前一笔共享全部触点
		{CustomerID: "u2", Time: 6000, Revenue: 7000},
	}
	tps, cvs = shuffleEvents(tps, cvs, 42)
	checkSummary(t, tps, cvs, 0, 1<<62)
}

// 场景二：报告范围切割（含起点、不含终点）与 30 天窗口边界（两端包含）。
func TestQueryRangeAndWindowBoundary(t *testing.T) {
	const L = WindowSeconds
	tps := []Touchpoint{
		{CustomerID: "u1", Time: 50, Channel: "sem", Campaign: "c1"},          // 范围起点之前，仍参与
		{CustomerID: "u1", Time: 200 - L, Channel: "social", Campaign: "c2"},  // 恰好窗口左端，合格
		{CustomerID: "u1", Time: 200 - L - 1, Channel: "edm", Campaign: "c3"}, // 窗口外 1 秒，不合格
		{CustomerID: "u1", Time: 300, Channel: "sem", Campaign: "c1"},         // 与转化同秒
	}
	cvs := []Conversion{
		{CustomerID: "u1", Time: 99, Revenue: 1000},   // 范围起点之前，不计入
		{CustomerID: "u1", Time: 200, Revenue: 10000}, // 范围内
		{CustomerID: "u1", Time: 300, Revenue: 5000},  // 恰好终点，不计入
	}
	tps, cvs = shuffleEvents(tps, cvs, 7)
	checkSummary(t, tps, cvs, 100, 300)

	// 显式断言边界：t=200 的转化只应看到 50 与 200-L 两个触点
	eng := New()
	eng.Load(tps, cvs)
	sum := eng.Summarize(200, 201)
	var gotSem, gotSocial, gotEdm float64
	for _, g := range sum.Groups {
		switch g.Channel {
		case "sem":
			gotSem = g.Amount
		case "social":
			gotSocial = g.Amount
		case "edm":
			gotEdm = g.Amount
		}
	}
	w1 := math.Exp2(-float64(200-50) / HalfLifeSeconds)
	w2 := math.Exp2(-float64(L) / HalfLifeSeconds)
	wantSem := 10000 * w1 / (w1 + w2)
	wantSocial := 10000 * w2 / (w1 + w2)
	if math.Abs(gotSem-wantSem) > 1e-6 || math.Abs(gotSocial-wantSocial) > 1e-6 {
		t.Errorf("boundary split: sem=%v want %v, social=%v want %v", gotSem, wantSem, gotSocial, wantSocial)
	}
	if gotEdm != 0 {
		t.Errorf("touchpoint 1s outside window must be excluded, got %v", gotEdm)
	}
	if sum.Unattributed != 0 {
		t.Errorf("unexpected unattributed %v", sum.Unattributed)
	}
}

// 场景三：跨 10 年空档后重新产生触达和转化；空档前的触点不得进入新窗口。
func TestMultiYearGap(t *testing.T) {
	const year = 365 * 24 * 3600
	base := int64(1_600_000_000)
	tps := []Touchpoint{
		{CustomerID: "u1", Time: base, Channel: "sem", Campaign: "old"},
		{CustomerID: "u1", Time: base + 10*year + 1000, Channel: "social", Campaign: "new"},
	}
	cvs := []Conversion{
		{CustomerID: "u1", Time: base + 100, Revenue: 8000},             // 空档前正常归因
		{CustomerID: "u1", Time: base + 5*year, Revenue: 9000},          // 空档中，无合格触点
		{CustomerID: "u1", Time: base + 10*year + 2000, Revenue: 11000}, // 空档后重新归因
	}
	tps, cvs = shuffleEvents(tps, cvs, 99)
	checkSummary(t, tps, cvs, 0, 1<<62)

	eng := New()
	eng.Load(tps, cvs)
	sum := eng.Summarize(0, 1<<62)
	if sum.Unattributed != 9000 {
		t.Errorf("mid-gap conversion should be fully unattributed, got %v", sum.Unattributed)
	}
	for _, g := range sum.Groups {
		if g.Campaign == "new" && math.Abs(g.Amount-11000) > 1e-6 {
			t.Errorf("new campaign should get full 11000, got %v", g.Amount)
		}
		if g.Campaign == "old" && math.Abs(g.Amount-8000) > 1e-6 {
			t.Errorf("old campaign should get full 8000, got %v", g.Amount)
		}
	}
}

// 同秒触点全部参与；无触点的转化进入未归因。
func TestSameSecondAndUnattributed(t *testing.T) {
	tps := []Touchpoint{
		{CustomerID: "u1", Time: 5000, Channel: "a", Campaign: "x"},
		{CustomerID: "u1", Time: 5000, Channel: "b", Campaign: "y"},
		{CustomerID: "u1", Time: 5000, Channel: "a", Campaign: "x"},
	}
	cvs := []Conversion{
		{CustomerID: "u1", Time: 5000, Revenue: 9000},
		{CustomerID: "u2", Time: 5000, Revenue: 3000}, // 无触点
	}
	tps, cvs = shuffleEvents(tps, cvs, 3)
	checkSummary(t, tps, cvs, 0, 1<<62)

	eng := New()
	eng.Load(tps, cvs)
	sum := eng.Summarize(0, 1<<62)
	if sum.Unattributed != 3000 {
		t.Errorf("unattributed: got %v want 3000", sum.Unattributed)
	}
	// 同秒权重均为 1：a/x 两票得 2/3，b/y 一票得 1/3
	for _, g := range sum.Groups {
		want := 3000.0
		if g.Channel == "a" {
			want = 6000.0
		}
		if math.Abs(g.Amount-want) > 1e-6 {
			t.Errorf("%v/%v: got %v want %v", g.Channel, g.Campaign, g.Amount, want)
		}
	}
}

// 回归：触点与转化交错，使转化在触点窗口中老化退出（覆盖第二遍扫描的过期路径）。
// 同时覆盖同一秒多触点、转化不截断后续窗口。
func TestInterleavedExpiryPath(t *testing.T) {
	tps := []Touchpoint{
		{CustomerID: "u1", Time: 10_000, Channel: "a", Campaign: "x"},
		{CustomerID: "u1", Time: 3_000_000, Channel: "b", Campaign: "y"}, // 与上一触点间隔超一个窗口
		{CustomerID: "u1", Time: 3_500_000, Channel: "a", Campaign: "x"},
		{CustomerID: "u1", Time: 9_000_000, Channel: "c", Campaign: "z"},
	}
	cvs := []Conversion{
		{CustomerID: "u1", Time: 5_000, Revenue: 4000},  // 无任何触点（第一个触点在其后）
		{CustomerID: "u1", Time: 20_000, Revenue: 6000}, // 仅 t=10000 的触点
		{CustomerID: "u1", Time: 3_200_000, Revenue: 8000},
		{CustomerID: "u1", Time: 3_600_000, Revenue: 10000},
		{CustomerID: "u1", Time: 9_000_500, Revenue: 12000},
	}
	tps, cvs = shuffleEvents(tps, cvs, 17)
	checkSummary(t, tps, cvs, 0, 1<<62)
	// 再切多个查询范围分别校验
	checkSummary(t, tps, cvs, 6_000, 3_300_000)
	checkSummary(t, tps, cvs, 3_300_000, 8_000_000)
	checkSummary(t, tps, cvs, 0, 6_000)
}

// 随机乱序压力：多客户、多组合，与参考实现全量对比。
func TestRandomizedAgainstReference(t *testing.T) {
	r := rand.New(rand.NewSource(2024))
	var tps []Touchpoint
	var cvs []Conversion
	customers := []string{"u1", "u2", "u3"}
	channels := []string{"sem", "social", "edm"}
	camps := []string{"c1", "c2"}
	for i := 0; i < 300; i++ {
		tps = append(tps, Touchpoint{
			CustomerID: customers[r.Intn(len(customers))],
			Time:       r.Int63n(5_000_000),
			Channel:    channels[r.Intn(len(channels))],
			Campaign:   camps[r.Intn(len(camps))],
		})
	}
	for i := 0; i < 200; i++ {
		cvs = append(cvs, Conversion{
			CustomerID: customers[r.Intn(len(customers))],
			Time:       r.Int63n(5_000_000),
			Revenue:    r.Int63n(1_000_000) + 1,
		})
	}
	for _, q := range [][2]int64{{0, 5_000_000}, {1_000_000, 3_000_000}, {2_500_000, 2_600_000}} {
		checkSummary(t, tps, cvs, q[0], q[1])
	}
}
