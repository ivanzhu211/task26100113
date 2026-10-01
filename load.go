package attribution

import "sort"

// Load 载入全部触点与转化事件。数据一次载入后不再修改，只能调用一次。
func (e *Engine) Load(tps []Touchpoint, cvs []Conversion) {
	for i := range tps {
		tp := &tps[i]
		key := groupKey{channel: tp.Channel, campaign: tp.Campaign}
		gi, ok := e.groupIdx[key]
		if !ok {
			gi = int32(len(e.groups))
			e.groupIdx[key] = gi
			e.groups = append(e.groups, key)
		}
		cd := e.customer(tp.CustomerID)
		cd.tpTimes = append(cd.tpTimes, tp.Time)
		cd.tpGroup = append(cd.tpGroup, gi)
	}
	for i := range cvs {
		cv := &cvs[i]
		cd := e.customer(cv.CustomerID)
		cd.cvTimes = append(cd.cvTimes, cv.Time)
		cd.cvRevenue = append(cd.cvRevenue, cv.Revenue)
	}
	for _, cd := range e.customers {
		cd.sortTouchpoints()
		cd.sortConversions()
		cd.computeWeights()
	}
}

func (e *Engine) customer(id string) *customerData {
	cd, ok := e.customers[id]
	if !ok {
		cd = &customerData{}
		e.customers[id] = cd
	}
	return cd
}

// sortTouchpoints 按时间升序排序，组索引随时间一起重排。
// 同秒触点的相对顺序不影响结果（权重相同）。
func (cd *customerData) sortTouchpoints() {
	idx := make([]int, len(cd.tpTimes))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		return cd.tpTimes[idx[a]] < cd.tpTimes[idx[b]]
	})
	times := make([]int64, len(idx))
	groups := make([]int32, len(idx))
	for i, j := range idx {
		times[i] = cd.tpTimes[j]
		groups[i] = cd.tpGroup[j]
	}
	cd.tpTimes = times
	cd.tpGroup = groups
}

// sortConversions 按时间升序排序转化，收入随时间一起重排。
func (cd *customerData) sortConversions() {
	idx := make([]int, len(cd.cvTimes))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		return cd.cvTimes[idx[a]] < cd.cvTimes[idx[b]]
	})
	times := make([]int64, len(idx))
	revenue := make([]int64, len(idx))
	for i, j := range idx {
		times[i] = cd.cvTimes[j]
		revenue[i] = cd.cvRevenue[j]
	}
	cd.cvTimes = times
	cd.cvRevenue = revenue
}
