package attribution

import (
	"math"
	"sort"
)

// Summarize 汇总 [start, end) 内（按转化时间筛选）的归因金额。
// 范围起点以前的触点，只要落在所选转化的窗口内，仍参与分配。
func (e *Engine) Summarize(start, end int64) Summary {
	amounts := make([]float64, len(e.groups))
	var unattr float64
	for _, cd := range e.customers {
		unattr += cd.accumulate(start, end, amounts)
	}
	groups := make([]GroupAmount, 0, len(e.groups))
	for i, a := range amounts {
		if a != 0 {
			groups = append(groups, GroupAmount{
				Channel:  e.groups[i].channel,
				Campaign: e.groups[i].campaign,
				Amount:   a,
			})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Channel != groups[j].Channel {
			return groups[i].Channel < groups[j].Channel
		}
		return groups[i].Campaign < groups[j].Campaign
	})
	return Summary{Groups: groups, Unattributed: unattr}
}

// accumulate 将该客户在 [start, end) 内的归因金额累加进 amounts，返回未归因金额。
//
// 对每个触点 t_i，其贡献为 Σ B_c · 2^(-(T_c - t_i)/H)，
// 其中 c 遍历 T_c ∈ [t_i, t_i+L] 的范围内转化。触点与转化各自有序，
// 用双指针滑动窗口维护 S = Σ B_c · 2^(-(T_c - tRef)/H)，均摊 O(1)。
func (cd *customerData) accumulate(start, end int64, amounts []float64) float64 {
	lo := sort.Search(len(cd.cvTimes), func(i int) bool { return cd.cvTimes[i] >= start })
	hi := sort.Search(len(cd.cvTimes), func(i int) bool { return cd.cvTimes[i] >= end })
	unattr := cd.unattr[hi] - cd.unattr[lo]

	// 范围内有合格触点的转化：(时间, B)
	var at []int64
	var ab []float64
	for j := lo; j < hi; j++ {
		if cd.cvAttr[j] {
			at = append(at, cd.cvTimes[j])
			ab = append(ab, cd.cvB[j])
		}
	}
	if len(at) == 0 {
		return unattr
	}

	cLo, cHi := 0, 0 // at 的滑动窗口 [cLo, cHi)
	var windowSum float64
	var windowRef int64
	hasWindow := false

	for i, ti := range cd.tpTimes {
		if ti > at[len(at)-1] {
			break // 之后的触点不可能落入任何范围内转化的窗口
		}
		// 过期 T_c < t_i 的转化
		for cLo < cHi && at[cLo] < ti {
			windowSum -= ab[cLo] * math.Exp2(float64(windowRef-at[cLo])/halfLife)
			cLo++
		}
		// 加入 T_c <= t_i + L 的转化
		for cHi < len(at) && at[cHi] <= ti+WindowSeconds {
			T := at[cHi]
			if !hasWindow {
				windowRef, hasWindow = T, true
			} else if T-windowRef > WindowSeconds {
				// 所有指数被约束在 ±2L/H 内；仅超出一个窗口才重定基准，
				// 避免每次加法都对总和做一次舍入乘法。
				if cLo < cHi {
					windowSum *= math.Exp2(float64(T-windowRef) / halfLife)
				} else {
					windowSum = 0
				}
				windowRef = T
			}
			windowSum += ab[cHi] * math.Exp2(float64(windowRef-T)/halfLife)
			cHi++
		}
		if cLo == cHi {
			windowSum = 0
			continue
		}
		amounts[cd.tpGroup[i]] += windowSum * math.Exp2(float64(ti-windowRef)/halfLife)
	}
	return unattr
}
