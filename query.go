package attribution

import (
	"math"
	"slices"
	"strings"
)

// halfLife 是以秒为单位的半衰期（浮点形式）。
const halfLife = float64(HalfLifeSeconds)

// rebaseThreshold 是滑动窗口参考时间的最大滞后半衰期个数。
// 参考时间落后当前时间超过它时重建窗口加权和，保证 2 的指数
// 始终落在 float64 安全范围内（即使存在多年空档）。
const rebaseThreshold = 400 * HalfLifeSeconds

// Query 汇总 [start, end) 内（按转化时间筛选，含起点不含终点）
// 全部转化的归因金额。范围起点之前的触点，只要落在所选转化的
// 30 天窗口内，仍然参与分配。
func (a *Attributor) Query(start, end int64) Report {
	if start >= end {
		return Report{}
	}
	amounts := make([]float64, len(a.groups))
	var unattrib float64
	for _, ce := range a.custs {
		unattrib += a.queryCustomer(ce, start, end, amounts)
	}
	var groups []GroupAmount
	for id, amt := range amounts {
		if amt != 0 {
			key := a.groups[id]
			groups = append(groups, GroupAmount{
				Channel:  key.channel,
				Campaign: key.campaign,
				Amount:   amt,
			})
		}
	}
	slices.SortFunc(groups, func(x, y GroupAmount) int {
		if c := strings.Compare(x.Channel, y.Channel); c != 0 {
			return c
		}
		return strings.Compare(x.Campaign, y.Campaign)
	})
	return Report{Groups: groups, Unattributed: unattrib}
}

// queryCustomer 汇总单个客户在 [start, end) 内的归因，把已归因金额累加进
// amounts（按组合 ID），返回未归因金额。
//
// 数学结构：转化 c（时间 T、收入 R、窗口总权重 Wc）把 R*2^(-(T-t)/h)/Wc
// 分给窗口内每个触点 t。本函数分两遍扫描：
//  1. 对每个转化，用触点滑动窗口求 Wc；
//  2. 对每个触点，用转化滑动窗口求它从所有转化分得的总额 f(t)，
//     再把 f(t) 累加到触点所属组合。
//
// 两遍都是 O(触点数+转化数)。加权和相对参考时间 base 计算并定期重建，
// 配合 Kahan 补偿求和保证 20 年跨度下的精度。
func (a *Attributor) queryCustomer(ce *customerEvents, start, end int64, amounts []float64) float64 {
	ct, cr := ce.convTimes, ce.convRev
	lo, _ := slices.BinarySearch(ct, start)
	hi, _ := slices.BinarySearch(ct, end)
	if lo == hi {
		return 0
	}
	tt, tg := ce.touchTimes, ce.touchGroup

	// 第一遍：为 [lo, hi) 内每个转化求窗口总权重 Wc。
	w := make([]float64, hi-lo)
	{
		wl, _ := slices.BinarySearch(tt, ct[lo]-WindowSeconds)
		wr := wl
		base := ct[lo]
		var sum, comp float64
		for j := lo; j < hi; j++ {
			T := ct[j]
			for wl < wr && tt[wl] < T-WindowSeconds {
				kahanAdd(&sum, &comp, -touchTerm(tt[wl], base))
				wl++
			}
			if T-base > rebaseThreshold {
				base = T
				sum, comp = 0, 0
				for i := wl; i < wr; i++ {
					kahanAdd(&sum, &comp, touchTerm(tt[i], base))
				}
			}
			for wr < len(tt) && tt[wr] <= T {
				kahanAdd(&sum, &comp, touchTerm(tt[wr], base))
				wr++
			}
			w[j-lo] = math.Exp2(-float64(T-base)/halfLife) * sum
		}
	}

	// 无合格触点的转化：全部收入计入未归因。
	var unattrib float64
	for j := lo; j < hi; j++ {
		if w[j-lo] == 0 {
			unattrib += float64(cr[j])
		}
	}

	// 第二遍：对每个触点求 f(t) 并累加到组合。
	// 只有落在 [start-Window, end) 的触点可能参与范围内转化。
	ti0, _ := slices.BinarySearch(tt, start-WindowSeconds)
	ti1, _ := slices.BinarySearch(tt, end)
	if ti0 == ti1 {
		return unattrib
	}
	cl, cr2 := lo, lo
	base := ct[lo]
	var sum, comp float64
	for i := ti0; i < ti1; i++ {
		t := tt[i]
		for cl < hi && ct[cl] < t {
			kahanAdd(&sum, &comp, -convTerm(ct[cl], cr[cl], w[cl-lo], base))
			cl++
		}
		if t-base > rebaseThreshold {
			base = t
			sum, comp = 0, 0
			for j := cl; j < cr2; j++ {
				kahanAdd(&sum, &comp, convTerm(ct[j], cr[j], w[j-lo], base))
			}
		}
		for cr2 < hi && ct[cr2] <= t+WindowSeconds {
			kahanAdd(&sum, &comp, convTerm(ct[cr2], cr[cr2], w[cr2-lo], base))
			cr2++
		}
		amounts[tg[i]] += math.Exp2(float64(t-base)/halfLife) * sum
	}
	return unattrib
}

// touchTerm 返回触点在权重前缀和中的项：2^((t-base)/h)。
func touchTerm(t, base int64) float64 {
	return math.Exp2(float64(t-base) / halfLife)
}

// convTerm 返回转化在触点贡献滑动和中的项：R*2^(-(T-base)/h)/Wc。
// Wc 为 0 的转化窗口内没有任何触点，不会被任何触点的窗口包含，
// 防御性地返回 0。
func convTerm(T, rev int64, w float64, base int64) float64 {
	if w == 0 {
		return 0
	}
	return float64(rev) * math.Exp2(-float64(T-base)/halfLife) / w
}

// kahanAdd 把 x 累加进 *sum，*comp 为 Kahan 补偿项。
func kahanAdd(sum, comp *float64, x float64) {
	y := x - *comp
	t := *sum + y
	*comp = (t - *sum) - y
	*sum = t
}
