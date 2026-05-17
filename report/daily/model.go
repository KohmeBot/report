package daily

import (
	"fmt"
	"strings"
	"time"
)

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
	MsgID        int64     // 消息ID,可以定位消息
	CreatedAt    time.Time `gorm:"index:idx_group_date,priority:3"`
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

	ShortCount  int            // 5字以内的短句数量（"哈哈" "对" "？"之类）
	FirstTime   time.Time      // 第一条发言的时间
	LastTime    time.Time      // 最后一条发言的时间
	NightOwl    bool           // 是否有凌晨0-4点的发言
	AllContents []string       `json:"-"` // 今天所有文本发言（用于关键词和代表发言采样）
	SampleMsgs  []GroupMessage // 最终筛选出的代表发言（4条）

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
	Start    time.Time
	End      time.Time
	Messages []GroupMessage `json:"-"`
	Summary  string         // AI摘要结果
}

// TimeStat 时间活跃度
type TimeStat struct {
	Time  time.Time
	Count int
}

// DailyReport 聚合结果
type DailyReport struct {
	GroupID       int64
	Date          string
	TotalMsg      int
	ActiveUsers   int
	HotPeriod     HotPeriod  // 热点话题
	StartTime     time.Time  // 开始时间
	EndTime       time.Time  // 结束时间
	TimeStats     []TimeStat // 按消息数降序
	UserStats     []UserStat // 按消息数降序
	TopKeywords   []WordStat
	FirstMessage  GroupMessage // 首条消息
	EndMessage    GroupMessage // 最后一条消息
	RepeatMessage WordStat     // 被复读最多次的消息
}

type WordStat struct {
	Word  string
	Count int
}

type DailyTheme struct {
	Theme             string `json:"theme"`
	Role              string `json:"role"`
	Style             string `json:"style"`
	UserFormat        string `json:"user_format"`
	GhostFormat       string `json:"ghost_format"`
	FirstHeader       string `json:"first_header"`
	EndHeader         string `json:"end_header"`
	MvpHeader         string `json:"mvp_header"`
	MomentHeader      string `json:"moment_header"`
	MomentFormat      string `json:"moment_format"`
	InteractionHeader string `json:"interaction_header"`
	InteractionFormat string `json:"interaction_format"`
	TriviaHeader      string `json:"trivia_header"`
	TriviaFormat      string `json:"trivia_format"`
	DiagnosisHeader   string `json:"diagnosis_header"`
	GhostHeader       string `json:"ghost_header"`

	Visual ThemeVisual `json:"visual"`
}

func (d *DailyTheme) String() string {
	return fmt.Sprintf(`
# 日报主题
日报主题：
%s
你的角色：
%s
整体写作风格：
%s
群友点评格式(这个只是参考,每个群友点评的句式必须要不一样):
%s
幽灵成员格式:
%s
互动描述格式:
%s
冷知识格式:
%s
禁止脱离以上世界观。
所有文案必须模仿上述格式结构与语气。
`,
		d.Theme,
		d.Role,
		d.Style,
		d.UserFormat,
		d.GhostFormat,
		d.InteractionFormat,
		d.TriviaFormat,
	)
}

type ThemeVisual struct {
	BgColor         string `json:"bg_color"`
	TextColor       string `json:"text_color"`
	AccentColor     string `json:"accent_color"`
	HeaderColor     string `json:"header_color"`
	FontStyle       string `json:"font_style"`
	BorderStyle     string `json:"border_style"`
	EmojiDecoration string `json:"emoji_decoration"`
}

type ReportJSON struct {
	Title      string `json:"title"`
	Opening    string `json:"opening"`
	FirstBlood struct {
		Nickname string `json:"nickname"`
		Time     string `json:"time"`
		Comment  string `json:"comment"`
	} `json:"first_blood"`
	LastWords struct {
		Nickname string `json:"nickname"`
		Time     string `json:"time"`
		Comment  string `json:"comment"`
	} `json:"last_words"`
	MVP []struct {
		Nickname string `json:"nickname"`
		Title    string `json:"title"`
		Comment  string `json:"comment"`
	} `json:"mvp"`
	Moment struct {
		Time    string `json:"time"`
		Comment string `json:"comment"`
	} `json:"moment"`
	Interaction struct {
		Type    string `json:"type"`
		Comment string `json:"comment"`
	} `json:"interaction"`
	Trivia []struct {
		Fact     string `json:"fact"`
		Question string `json:"question"`
	} `json:"trivia"`
	Diagnosis string `json:"diagnosis"`
	Ghosts    struct {
		Names   []string `json:"names"`
		Comment string   `json:"comment"`
	} `json:"ghosts"`
}

func (r ReportJSON) String(theme *DailyTheme) string {
	var sb strings.Builder

	sb.WriteString(r.Opening + "\n")

	// MVP
	if len(r.MVP) > 0 {
		sb.WriteString(theme.MvpHeader + "\n")
		for i, m := range r.MVP {
			sb.WriteString(fmt.Sprintf("%d. %s · %s\n%s\n\n", i+1, m.Nickname, m.Title, m.Comment))
		}
	}

	// 关键时刻
	if r.Moment.Comment != "" {
		sb.WriteString(theme.MomentHeader + "\n")
		sb.WriteString(fmt.Sprintf("[%s] %s\n\n", r.Moment.Time, r.Moment.Comment))
	}

	// 社交图谱
	if r.Interaction.Comment != "" {
		sb.WriteString(theme.InteractionHeader + "\n")
		sb.WriteString(fmt.Sprintf("%s\n%s\n\n", r.Interaction.Type, r.Interaction.Comment))
	}

	// 冷知识
	if len(r.Trivia) > 0 {
		sb.WriteString(theme.TriviaHeader + "\n")
		for _, t := range r.Trivia {
			sb.WriteString(t.Fact + "\n")
			if t.Question != "" {
				sb.WriteString(t.Question + "\n")
			}
			sb.WriteString("\n")
		}
	}

	// 群体诊断
	if r.Diagnosis != "" {
		sb.WriteString(theme.DiagnosisHeader + "\n")
		sb.WriteString(r.Diagnosis + "\n\n")
	}

	// 失踪人口
	if len(r.Ghosts.Names) > 0 {
		sb.WriteString(theme.GhostHeader + "\n")
		sb.WriteString(strings.Join(r.Ghosts.Names, " / ") + "\n")
		if r.Ghosts.Comment != "" {
			sb.WriteString(r.Ghosts.Comment + "\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
