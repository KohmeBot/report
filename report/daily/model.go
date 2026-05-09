package daily

import "time"

type GroupDailyStat struct {
	ID      uint   `gorm:"primarykey"`
	GroupID int64  `gorm:"uniqueIndex:idx_group_day,priority:1"`
	Date    string `gorm:"uniqueIndex:idx_group_day,priority:2"`
	Data    string // 整个 DailyReport 的 JSON
	Report  string // ai生成的report结果
	Theme   string // theme json
}

// GroupMessage 原始消息（流水表）
type GroupMessage struct {
	ID           uint  `gorm:"primarykey"`
	GroupID      int64 `gorm:"index:idx_group_date,priority:1"`
	UserID       int64 `gorm:"index:idx_group_date,priority:2"`
	TargetUserID int64 // 群内@对方的ID或者是reply的ID
	Nickname     string
	Content      string
	MsgType      string    // text/image/poke/mixed
	Hour         int       // 0-23，方便GROUP BY
	MsgID        int64     // 消息ID,可以定位消息
	CreatedAt    time.Time `gorm:"index:idx_group_date,priority:3"`
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
	AllContents []string `json:"-"` // 今天所有文本发言（用于关键词和代表发言采样）
	SampleMsgs  []string // 最终筛选出的代表发言（4条）

	// 复读
	RepeatMsg   string // 今天复读次数最多的那条原文
	RepeatCount int    // 复读了几次

	// 回复行为
	ReplyCount int // 引用回复别人的次数（不是at，是reply）
	BeReplied  int // 被别人引用回复的次数（说明发言有人接）

	// 情绪特征
	ExclamCount   int // 感叹号数量（！or!），衡量激动程度
	QuestionCount int // 问号数量，衡量迷茫/好奇程度
	EllipsisCount int // 省略号数量（……），衡量沉默/无奈程度

	// 发言节奏
	BurstCount  int // 连发行为次数：60秒内连发3条以上算一次burst
	LonelyCount int // 发出后5分钟内无人回应的消息数（孤独指数）

	// 词汇特征
	VocabSize int // 今天用了多少种不同的词（去重后），衡量表达丰富度
	AvgMsgLen int // 平均发言字数
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

type DailyTheme struct {
	Theme             string `json:"theme"`
	Role              string `json:"role"`
	Style             string `json:"style"`
	Opening           string `json:"opening"`
	UserFormat        string `json:"user_format"`
	GhostFormat       string `json:"ghost_format"`
	MvpHeader         string `json:"mvp_header"`
	MomentHeader      string `json:"moment_header"`
	MomentFormat      string `json:"moment_format"`
	InteractionHeader string `json:"interaction_header"`
	InteractionFormat string `json:"interaction_format"`
	TriviaHeader      string `json:"trivia_header"`
	TriviaFormat      string `json:"trivia_format"`
	DiagnosisHeader   string `json:"diagnosis_header"`
	GhostHeader       string `json:"ghost_header"`
}
