package daily

import "time"

// GroupMessage 原始消息（流水表）
type GroupMessage struct {
	ID        uint  `gorm:"primarykey"`
	GroupID   int64 `gorm:"index:idx_group_date,priority:1"`
	UserID    int64 `gorm:"index:idx_group_date,priority:2"`
	Nickname  string
	Content   string
	MsgType   string    // text/image/poke/mixed
	Hour      int       // 0-23，方便GROUP BY
	MsgID     int64     // 消息ID,可以定位消息
	CreatedAt time.Time `gorm:"index:idx_group_date,priority:3"`
}

// UserStat 聚合后的单个用户数据（内存结构，不落库）
type UserStat struct {
	UserID      int64
	Nickname    string
	MsgCount    int      // 消息数量
	ImageCount  int      // 图片or表情包数量
	PokeCount   int      // 戳一戳数量
	AtCount     int      // at别人的数量
	ShortCount  int      // 5字以内的短句数量（"哈哈" "对" "？"之类）
	FirstHour   int      // 第一条发言的小时
	LastHour    int      // 最后一条发言的小时
	NightOwl    bool     // 是否有凌晨0-4点的发言
	AllContents []string // 今天所有文本发言（用于关键词和代表发言采样）
	SampleMsgs  []string // 最终筛选出的代表发言（4条）
}

// HourStat 小时活跃度
type HourStat struct {
	Hour  int
	Count int
}

// DailyReport 聚合结果
type DailyReport struct {
	GroupID     int64
	Date        string
	TotalMsg    int
	ActiveUsers int
	HourStats   []HourStat // 按消息数降序
	UserStats   []UserStat // 按消息数降序
	GhostUsers  []string   // 今日0发言（从群成员列表对比，可选）
	TopKeywords []KeywordStat
}

type KeywordStat struct {
	Word  string
	Count int
}
