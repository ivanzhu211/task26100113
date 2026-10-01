// Package attribution 实现营销收入的时间衰减归因。
//
// 模型固定为 7 天半衰期的时间衰减：触点距转化 d 秒时，
// 权重为 2^(-d/604800)。每笔转化的合格触点窗口为
// [转化时间-2592000, 转化时间]，两端包含。
package attribution

// 业务常量。
const (
	// WindowSeconds 是合格触点窗口长度：30 天（秒）。
	WindowSeconds int64 = 2592000
	// HalfLifeSeconds 是衰减半衰期：7 天（秒）。
	HalfLifeSeconds int64 = 604800
)

// Touchpoint 是一次营销触点事件。Time 为 UTC 秒级时间。
type Touchpoint struct {
	CustomerID string
	Time       int64
	Channel    string
	Campaign   string
}

// Conversion 是一笔转化事件。RevenueCents 为整数分收入。
type Conversion struct {
	CustomerID   string
	Time         int64
	RevenueCents int64
}

// GroupAmount 是一个渠道+活动组合的归因金额（单位：分，允许小数）。
type GroupAmount struct {
	Channel  string
	Campaign string
	Amount   float64
}

// Report 是一次范围查询的结果。金额单位均为分。
type Report struct {
	// Groups 按 (Channel, Campaign) 字典序排列，只含金额非零的组合。
	Groups []GroupAmount
	// Unattributed 为无合格触点的转化收入之和。
	Unattributed float64
}

// Total 返回已归因金额与未归因金额之和。
func (r Report) Total() float64 {
	sum := r.Unattributed
	for _, g := range r.Groups {
		sum += g.Amount
	}
	return sum
}
