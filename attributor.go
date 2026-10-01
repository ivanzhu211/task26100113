package attribution

import (
	"slices"
)

// Attributor 保存载入的事件数据，支持多次范围查询。
// 数据载入后不再修改，全部保存在内存中。
type Attributor struct {
	groups   []groupKey                 // 组合 ID -> 渠道+活动
	groupIDs map[groupKey]int32         // 渠道+活动 -> 组合 ID
	custs    map[string]*customerEvents // 客户 -> 事件
}

// groupKey 是 (渠道, 活动) 组合的内部键。
type groupKey struct {
	channel  string
	campaign string
}

// customerEvents 是单个客户的事件，按时间升序排列。
type customerEvents struct {
	touchTimes []int64 // 触点时间，升序
	touchGroup []int32 // 触点对应的组合 ID
	convTimes  []int64 // 转化时间，升序
	convRev    []int64 // 转化收入（分）
}

// New 创建一个空的 Attributor。
func New() *Attributor {
	return &Attributor{
		groupIDs: make(map[groupKey]int32),
		custs:    make(map[string]*customerEvents),
	}
}

// Load 一次性载入全部触点与转化事件。输入顺序任意，事件已去重。
// 载入后数据不再修改。
func (a *Attributor) Load(touches []Touchpoint, convs []Conversion) {
	type touchRec struct {
		time  int64
		group int32
	}
	touchBuf := make(map[string][]touchRec, len(a.custs))
	for _, tp := range touches {
		touchBuf[tp.CustomerID] = append(touchBuf[tp.CustomerID],
			touchRec{time: tp.Time, group: a.groupID(tp.Channel, tp.Campaign)})
	}
	for id, recs := range touchBuf {
		slices.SortFunc(recs, func(x, y touchRec) int {
			return cmpInt64(x.time, y.time)
		})
		ce := a.customer(id)
		for _, r := range recs {
			ce.touchTimes = append(ce.touchTimes, r.time)
			ce.touchGroup = append(ce.touchGroup, r.group)
		}
	}

	convBuf := make(map[string][]Conversion, len(a.custs))
	for _, cv := range convs {
		convBuf[cv.CustomerID] = append(convBuf[cv.CustomerID], cv)
	}
	for id, recs := range convBuf {
		slices.SortFunc(recs, func(x, y Conversion) int {
			return cmpInt64(x.Time, y.Time)
		})
		ce := a.customer(id)
		for _, r := range recs {
			ce.convTimes = append(ce.convTimes, r.Time)
			ce.convRev = append(ce.convRev, r.RevenueCents)
		}
	}
}

// groupID 返回组合的内部 ID，不存在时新建。
func (a *Attributor) groupID(channel, campaign string) int32 {
	key := groupKey{channel: channel, campaign: campaign}
	if id, ok := a.groupIDs[key]; ok {
		return id
	}
	id := int32(len(a.groups))
	a.groups = append(a.groups, key)
	a.groupIDs[key] = id
	return id
}

// customer 返回客户的事件存储，不存在时新建。
func (a *Attributor) customer(id string) *customerEvents {
	if ce, ok := a.custs[id]; ok {
		return ce
	}
	ce := &customerEvents{}
	a.custs[id] = ce
	return ce
}

func cmpInt64(x, y int64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	default:
		return 0
	}
}
