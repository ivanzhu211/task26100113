package attribution

// 业务常量：合格窗口与半衰期，单位均为 UTC 秒。
const (
	WindowSeconds   = 2592000 // 30 天合格窗口，两端包含
	HalfLifeSeconds = 604800  // 7 天半衰期
)

// Touchpoint 触点事件。Time 为 UTC 秒级时间。
type Touchpoint struct {
	CustomerID string
	Time       int64
	Channel    string
	Campaign   string
}

// Conversion 转化事件。Revenue 为整数分。
type Conversion struct {
	CustomerID string
	Time       int64
	Revenue    int64
}

// GroupAmount 单个渠道+活动组合的归因金额，单位为小数分。
type GroupAmount struct {
	Channel  string
	Campaign string
	Amount   float64
}

// Summary 一次范围查询的结果：各组合归因金额 + 未归因金额。
type Summary struct {
	Groups       []GroupAmount
	Unattributed float64
}
