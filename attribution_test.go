package attribution

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// bruteForce 是测试用的参考实现：对范围内每笔转化逐一遍历全部触点，
// 直接按业务定义计算权重与分配，不做任何性能优化。
func bruteForce(touches []Touchpoint, convs []Conversion, start, end int64) Report {
	type key struct{ channel, campaign string }
	amounts := map[key]float64{}
	var un float64
	for _, cv := range convs {
		if cv.Time < start || cv.Time >= end {
			continue
		}
		var wsum float64
		var hit []int
		for i, tp := range touches {
			if tp.CustomerID != cv.CustomerID {
				continue
			}
			d := cv.Time - tp.Time
			if d < 0 || d > WindowSeconds {
				continue
			}
			hit = append(hit, i)
			wsum += math.Exp2(-float64(d) / float64(HalfLifeSeconds))
		}
		if len(hit) == 0 {
			un += float64(cv.RevenueCents)
			continue
		}
		for _, i := range hit {
			tp := touches[i]
			d := cv.Time - tp.Time
			w := math.Exp2(-float64(d) / float64(HalfLifeSeconds))
			amounts[key{tp.Channel, tp.Campaign}] += float64(cv.RevenueCents) * w / wsum
		}
	}
	var groups []GroupAmount
	for k, v := range amounts {
		if v != 0 {
			groups = append(groups, GroupAmount{Channel: k.channel, Campaign: k.campaign, Amount: v})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Channel != groups[j].Channel {
			return groups[i].Channel < groups[j].Channel
		}
		return groups[i].Campaign < groups[j].Campaign
	})
	return Report{Groups: groups, Unattributed: un}
}

// 容差：每组 0.0001 元 = 0.01 分；总额 0.001 元 = 0.1 分。
const (
	groupTol = 0.01
	totalTol = 0.1
)

// checkReport 校验 got 与参考 want 在规定容差内一致，并校验收入守恒。
func checkReport(t *testing.T, got, want Report, totalRev float64) {
	t.Helper()
	wantMap := map[[2]string]float64{}
	for _, g := range want.Groups {
		wantMap[[2]string{g.Channel, g.Campaign}] = g.Amount
	}
	gotMap := map[[2]string]float64{}
	for _, g := range got.Groups {
		gotMap[[2]string{g.Channel, g.Campaign}] = g.Amount
	}
	keys := map[[2]string]bool{}
	for k := range wantMap {
		keys[k] = true
	}
	for k := range gotMap {
		keys[k] = true
	}
	for k := range keys {
		diff := math.Abs(gotMap[k] - wantMap[k])
		if diff > groupTol {
			t.Errorf("组 %v: got %.6f, want %.6f, diff %.6f > %v", k, gotMap[k], wantMap[k], diff, groupTol)
		}
	}
	if d := math.Abs(got.Unattributed - want.Unattributed); d > groupTol {
		t.Errorf("未归因: got %.6f, want %.6f, diff %.6f > %v", got.Unattributed, want.Unattributed, d, groupTol)
	}
	if d := math.Abs(got.Total() - want.Total()); d > totalTol {
		t.Errorf("总额与参考不一致: got %.6f, want %.6f", got.Total(), want.Total())
	}
	if d := math.Abs(got.Total() - totalRev); d > totalTol {
		t.Errorf("收入不守恒: 报告总额 %.6f, 范围内转化收入 %.6f, diff %.6f > %v", got.Total(), totalRev, d, totalTol)
	}
}

func totalRevenue(convs []Conversion, start, end int64) float64 {
	var sum float64
	for _, cv := range convs {
		if cv.Time >= start && cv.Time < end {
			sum += float64(cv.RevenueCents)
		}
	}
	return sum
}

// queryAndCheck 对同一数据分别用库与参考实现汇总 [start,end)，并比对。
func queryAndCheck(t *testing.T, a *Attributor, touches []Touchpoint, convs []Conversion, start, end int64) {
	t.Helper()
	got := a.Query(start, end)
	want := bruteForce(touches, convs, start, end)
	checkReport(t, got, want, totalRevenue(convs, start, end))
}

// TestSharedTouchesShuffled 覆盖乱序输入下多次转化共享触点：
// 同一触点参与多笔转化，同秒触点全部参与，转化之间互不影响。
func TestSharedTouchesShuffled(t *testing.T) {
	const base = int64(1_700_000_000)
	touches := []Touchpoint{
		{CustomerID: "u1", Time: base, Channel: "搜索", Campaign: "品牌词"},
		{CustomerID: "u1", Time: base + 86400, Channel: "社交", Campaign: "拉新"},
		{CustomerID: "u1", Time: base + 86400, Channel: "搜索", Campaign: "品牌词"}, // 同秒触点
		{CustomerID: "u1", Time: base + 3*86400, Channel: "邮件", Campaign: "召回"},
		{CustomerID: "u1", Time: base + 10*86400, Channel: "社交", Campaign: "拉新"},
		{CustomerID: "u2", Time: base + 86400, Channel: "社交", Campaign: "拉新"},
		{CustomerID: "u2", Time: base + 20*86400, Channel: "搜索", Campaign: "竞品词"},
	}
	convs := []Conversion{
		{CustomerID: "u1", Time: base + 5*86400, RevenueCents: 100000},
		{CustomerID: "u1", Time: base + 5*86400, RevenueCents: 50000}, // 同秒转化
		{CustomerID: "u1", Time: base + 15*86400, RevenueCents: 200000},
		{CustomerID: "u1", Time: base + 40*86400, RevenueCents: 999999}, // 窗口只覆盖部分触点
		{CustomerID: "u2", Time: base + 25*86400, RevenueCents: 80000},
		{CustomerID: "u3", Time: base + 2*86400, RevenueCents: 60000}, // 无触点，未归因
	}

	// 乱序（固定种子，确定性）。
	rng := rand.New(rand.NewSource(42))
	shuffledT := append([]Touchpoint(nil), touches...)
	rng.Shuffle(len(shuffledT), func(i, j int) { shuffledT[i], shuffledT[j] = shuffledT[j], shuffledT[i] })
	shuffledC := append([]Conversion(nil), convs...)
	rng.Shuffle(len(shuffledC), func(i, j int) { shuffledC[i], shuffledC[j] = shuffledC[j], shuffledC[i] })

	a := New()
	a.Load(shuffledT, shuffledC)

	// 全范围与若干子范围。
	queryAndCheck(t, a, touches, convs, base-1, base+60*86400)
	queryAndCheck(t, a, touches, convs, base+5*86400, base+15*86400)
	queryAndCheck(t, a, touches, convs, base+15*86400, base+40*86400)
	queryAndCheck(t, a, touches, convs, base+40*86400, base+40*86400+1)
}

// TestWindowAndRangeBoundary 覆盖 30 天窗口边界与报告范围切割：
// 窗口两端包含；范围含起点不含终点；范围起点之前的触点仍参与分配。
func TestWindowAndRangeBoundary(t *testing.T) {
	const T0 = int64(1_700_000_000)
	touches := []Touchpoint{
		{CustomerID: "u1", Time: T0 - WindowSeconds - 1, Channel: "搜索", Campaign: "窗外"}, // 窗口外 1 秒
		{CustomerID: "u1", Time: T0 - WindowSeconds, Channel: "社交", Campaign: "窗口左边界"},  // 恰好 30 天前
		{CustomerID: "u1", Time: T0 - 100, Channel: "邮件", Campaign: "中间"},               //
		{CustomerID: "u1", Time: T0, Channel: "搜索", Campaign: "同秒"},                     // 与转化同秒
		{CustomerID: "u1", Time: T0 + 1, Channel: "搜索", Campaign: "转化之后"},               // 转化之后不参与
		{CustomerID: "u1", Time: T0 + WindowSeconds, Channel: "社交", Campaign: "下一窗左边界"}, // 相对第二笔转化恰好 30 天前
	}
	convs := []Conversion{
		{CustomerID: "u1", Time: T0, RevenueCents: 1000000},
		{CustomerID: "u1", Time: T0 + WindowSeconds, RevenueCents: 500000},
		{CustomerID: "u1", Time: T0 + 2*WindowSeconds, RevenueCents: 250000},
	}
	a := New()
	a.Load(touches, convs)

	// 全范围。
	queryAndCheck(t, a, touches, convs, T0-WindowSeconds-10, T0+3*WindowSeconds)
	// 范围切割：只含第一笔转化。
	queryAndCheck(t, a, touches, convs, T0, T0+1)
	// 范围起点之后的转化仍使用起点之前的触点。
	queryAndCheck(t, a, touches, convs, T0+1, T0+2*WindowSeconds+1)
	// 范围终点不含：第二笔转化恰好落在终点。
	queryAndCheck(t, a, touches, convs, T0, T0+WindowSeconds)
	// 空范围。
	if r := a.Query(T0+5, T0+5); len(r.Groups) != 0 || r.Unattributed != 0 {
		t.Errorf("空范围应返回空报告, got %+v", r)
	}
}

// TestMultiYearGap 覆盖跨多年空档：空档两侧的事件权重不能丢失或溢出，
// 空档后新产生的触达与转化正常归因。
func TestMultiYearGap(t *testing.T) {
	const year = int64(365 * 86400)
	base := int64(1_267_000_000) // 约 2010 年
	gap := 10*year + 123*86400   // 超过 10 年的空档
	touches := []Touchpoint{
		{CustomerID: "u1", Time: base, Channel: "搜索", Campaign: "早期"},
		{CustomerID: "u1", Time: base + 5*86400, Channel: "社交", Campaign: "早期"},
		// 空档之后：
		{CustomerID: "u1", Time: base + gap, Channel: "搜索", Campaign: "回归"},
		{CustomerID: "u1", Time: base + gap + 20*86400, Channel: "邮件", Campaign: "回归"},
	}
	convs := []Conversion{
		{CustomerID: "u1", Time: base + 10*86400, RevenueCents: 300000},
		// 空档后的转化：窗口够不到 10 年前的触点。
		{CustomerID: "u1", Time: base + gap + 25*86400, RevenueCents: 700000},
		// 空档正中间、前后都无触点的转化：全部未归因。
		{CustomerID: "u1", Time: base + gap/2, RevenueCents: 150000},
	}
	a := New()
	a.Load(touches, convs)

	queryAndCheck(t, a, touches, convs, base-year, base+gap+year)
	// 只查空档后的范围。
	queryAndCheck(t, a, touches, convs, base+gap-WindowSeconds, base+gap+year)
	// 只查空档前的范围。
	queryAndCheck(t, a, touches, convs, base-year, base+20*86400)
}
