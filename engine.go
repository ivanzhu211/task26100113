package attribution

// Engine 内存中的时间衰减归因引擎。数据一次载入后不可变。
type Engine struct {
	groups    []groupKey
	groupIdx  map[groupKey]int32
	customers map[string]*customerData
}

type groupKey struct {
	channel  string
	campaign string
}

type customerData struct {
	tpTimes   []int64 // 按时间升序
	tpGroup   []int32 // 与 tpTimes 对齐
	cvTimes   []int64 // 按时间升序
	cvRevenue []int64
	cvB       []float64 // 每笔转化的 R/W（无合格触点时未使用）
	cvAttr    []bool    // 是否有合格触点
	unattr    []float64 // 未归因收入前缀和
}

// New 创建空引擎。
func New() *Engine {
	return &Engine{
		groupIdx:  make(map[groupKey]int32),
		customers: make(map[string]*customerData),
	}
}
