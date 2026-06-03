package render

type ReportData struct {
	Report       *DailyReport  // 日常数据
	Topics       []*TopicItem  // 话题数据
	UserData     []*UserItem   // 用户数据
	GoldenData   []*GoldenItem // 金句数据
	GroupQuality *GroupQuality // 群聊质量分析
}

type DailyReport struct {
	Title     string `json:"title"` // 群聊标题
	GroupName string `json:"group_name"`
	GroupID   string `json:"group_id"`
	Date      string `json:"date"` // YYYY年-MM月-DD日

	// ── Header stats ──────────────────────────
	Stats Stats `json:"stats"`
}

// Stats contains group activity statistics shown in the header.
type Stats struct {
	TotalMessages      int        `json:"total_messages"`      // 消息总数
	ActiveUsers        int        `json:"active_users"`        // 参与人数
	CharCount          int        `json:"char_count"`          // 字符总数
	MemeCount          int        `json:"meme_count"`          // 表情包总数
	HighLightTime      string     `json:"high_light_time"`     // 高峰时段 e.g. "22:00~2:00"
	HourlyDistribution []HourSlot `json:"hourly_distribution"` // 24小时活动轨迹 (可能会跨天 比如4:00-次日3:59)
}

// HourSlot is one column in the 24-hour activity bar chart.
type HourSlot struct {
	Hour       int     `json:"hour"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"` // 0-100
}

type TopicItem struct {
	Index        int     `json:"index"`        // 1-based
	Topic        string  `json:"topic"`        // 话题昵称
	Contributors []int64 `json:"contributors"` // 话题的参与者
	Detail       string  `json:"detail"`       // 话题的详细描述,如果涉及到用户，会有[用户ID]，这里要渲染成一个头像+昵称的小样式
}

type UserItem struct {
	User   int64  `json:"user"`   // 用户数据 渲染成头像+昵称
	Title  string `json:"title"`  // 称号
	Mbti   string `json:"mbti"`   // 用户MBTI
	Reason string `json:"reason"` //  获得称号的原因
}

type GoldenItem struct {
	Content string `json:"content"` // 金句内容 整体渲染成头像+昵称+气泡
	Sender  int64  `json:"sender"`  // 发送者 渲染成头像+昵称
	Reason  string `json:"reason"`  // 评选原因，AI锐评
	Time    string `json:"time"`    // 发送时间
}

type GroupQuality struct {
	Title      string      `json:"title"`      // 今日群聊主题
	Subtitle   string      `json:"subtitle"`   // 副标题
	Dimensions []Dimension `json:"dimensions"` // 群聊维度
	Summary    string      `json:"summary"`    // 群聊总结 这部分体现为AI说的话，也就是AI头像加一个气泡
	AIUser     int64       `json:"-"`          // AI的用户User
}

type Dimension struct {
	Name       string  `json:"name"`       // 维度名称
	Percentage float64 `json:"percentage"` // 占比 总和100
	Comment    string  `json:"comment"`    // 维度锐评
}
