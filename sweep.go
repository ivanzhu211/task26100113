package attribution

import "math"

const halfLife = float64(HalfLifeSeconds)

// computeWeights 第一遍扫描：为每笔转化计算合格触点权重和 W，
// 记录 B = R/W（无合格触点时标记未归因），并累计未归因收入前缀和。
//
// 滑动窗口维护 S = Σ 2^(-(tRef - t_j)/H)，其中 t_j 为窗口内触点时间。
// 转化 T 时刻的权重和 W = S · 2^(-(T - tRef)/H)。
// 所有指数均被窗口长度约束，20 年空档只会产生可忽略的下溢，不会溢出。
func (cd *customerData) computeWeights() {
	n := len(cd.cvTimes)
	cd.cvB = make([]float64, n)
	cd.cvAttr = make([]bool, n)
	cd.unattr = make([]float64, n+1)

	head, tail := 0, 0 // tpTimes 的滑动窗口 [head, tail)
	var sum float64
	var tRef int64
	hasRef := false
	var unattrSum float64

	for j, T := range cd.cvTimes {
		// 加入所有 t <= T 的触点（同秒触点全部参与）
		for tail < len(cd.tpTimes) && cd.tpTimes[tail] <= T {
			t := cd.tpTimes[tail]
			if !hasRef {
				tRef, hasRef = t, true
			}
			if t-tRef > WindowSeconds {
				// 阈值式重定基准，指数始终被约束在 ±2L/H 内
				sum *= math.Exp2(float64(tRef-t) / halfLife)
				tRef = t
			}
			sum += math.Exp2(float64(t-tRef) / halfLife) // tRef == t 时为 1
			tail++
		}
		// 过期 t < T - WindowSeconds 的触点（窗口左端包含，故严格小于才过期）
		for head < tail && cd.tpTimes[head] < T-WindowSeconds {
			sum -= math.Exp2(float64(cd.tpTimes[head]-tRef) / halfLife)
			head++
		}
		if head == tail {
			sum = 0 // 消除浮点残差
		}
		W := 0.0
		if tail > head {
			W = sum * math.Exp2(float64(tRef-T)/halfLife)
		}
		if W > 0 {
			cd.cvAttr[j] = true
			cd.cvB[j] = float64(cd.cvRevenue[j]) / W
		} else {
			unattrSum += float64(cd.cvRevenue[j])
		}
		cd.unattr[j+1] = unattrSum
	}
}
