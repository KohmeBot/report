package daily

import (
	"time"
)

type GroupDailyStat struct {
	ID      uint   `gorm:"primarykey"`
	GroupID int64  `gorm:"uniqueIndex:idx_group_day,priority:1"`
	Date    string `gorm:"uniqueIndex:idx_group_day,priority:2"`
	Data    string // 整个 AggregateData 的 JSON
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
	MsgID        int64     // 消息ID,可以定位消息
	CreatedAt    time.Time `gorm:"index:idx_group_date,priority:3"`
	Url          string    // url
}

type SpecifyTheme struct {
	ID          uint `gorm:"primarykey"`
	ThemeString string
	Date        string `gorm:"uniqueIndex:idx_specify_theme_day,priority:1"`
}

type User struct {
	UserId   int64
	Nickname string
}

// UserStat 聚合后的单个用户数据（内存结构，不落库）
type UserStat struct {
	User
	MsgCount     int            // 消息数量
	MsgTypeCount map[string]int // 消息类型的数量

	ShortCount  int      // 5字以内的短句数量（"哈哈" "对" "？"之类）
	NightOwl    bool     // 是否有凌晨0-4点的发言
	AllContents []string `json:"-"` // 今天所有文本发言（用于关键词和代表发言采样）

	// 复读
	RepeatMsg   string // 今天复读次数最多的那条原文
	RepeatCount int    // 复读了几次

	// 互动行为
	BeReplied          map[User]int            // 被别人互动的次数 key: 对方的ID value: 次数
	BeRepliedMessage   map[User][]GroupMessage `json:"-"` // 被别人互动消息 key: 对方ID value: 对应的互动消息
	InteractionCount   map[User]int            // 互动别人的次数 key: 互动对象的ID value: 次数
	InteractionMessage map[User][]GroupMessage `json:"-"` // 互动消息 key: 互动对象ID value: 对应的互动消息

	// 情绪特征
	ExclamCount   int // 感叹号数量（！or!），衡量激动程度
	QuestionCount int // 问号数量，衡量迷茫/好奇程度
	EllipsisCount int // 省略号数量（……），衡量沉默/无奈程度

	// 发言节奏
	Rhythm      RhythmStat // 发言节奏
	LonelyCount int        // 发出后5分钟内无人回应的消息数（孤独指数）

	// 词汇特征
	VocabSize int // 今天用了多少种不同的词（去重后），衡量表达丰富度
	AvgMsgLen int // 平均发言字数

	FirstMessage GroupMessage // 第一条消息
	EndMessage   GroupMessage // 最后一条消息
}

func (s *UserStat) totalInteraction() int {
	total := 0
	for _, count := range s.InteractionCount {
		total += count
	}
	return total
}

// RhythmStat 发言节奏统计
type RhythmStat struct {
	BurstCount     int           // 爆发次数：5分钟内发了5条以上算一次爆发
	BurstMaxSize   int           // 最大单次爆发条数
	LongestSilence time.Duration // 两条发言之间最长的沉默时间
	AvgInterval    time.Duration // 平均发言间隔
	ActivePeriods  int           // 活跃时间段数量（间隔超过30分钟算切换一次）
}

// ChatSegment 自然话题段
type ChatSegment struct {
	Start    time.Time
	End      time.Time
	Messages []GroupMessage
}

type HotPeriod struct {
	Start time.Time
	End   time.Time
}

// TimeStat 时间活跃度
type TimeStat struct {
	Time  time.Time
	Count int
}

// AggregateData 聚合结果
type AggregateData struct {
	GroupMessages  []GroupMessage
	GroupID        int64
	Date           string
	TotalMsg       int          // 总消息数
	ActiveUsers    int          // 活跃用户数
	TotalCharCount int          // 总字符数
	TotalMemeCount int          // 总表情数
	HotPeriod      HotPeriod    // 热点话题
	StartTime      time.Time    // 开始时间
	EndTime        time.Time    // 结束时间
	TimeStats      []TimeStat   // 按消息数降序
	UserStats      []UserStat   // 按消息数降序
	FirstMessage   GroupMessage // 首条消息
	EndMessage     GroupMessage // 最后一条消息
	RepeatMessage  RepeatStat   // 被复读最多次的消息
}

type WordStat struct {
	Word  string
	Count int
}

type RepeatStat struct {
	Content     string
	Count       int
	FirstSender User      // 发起人
	StartTime   time.Time // 复读开始时间
}

type Prompts struct {
	TopicPrompt   string
	UserPrompt    string
	GoldenPrompt  string
	QualityPrompt string
}
