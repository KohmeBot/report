package daily

import (
	"gorm.io/gorm"
	"sort"
)

type Aggregator struct {
	db *gorm.DB
}

func NewAggregator(db *gorm.DB) *Aggregator {
	return &Aggregator{db: db}
}

// Aggregate 对指定群、指定日期做全量聚合，返回DailyReport
func (a *Aggregator) Aggregate(groupID int64, date string) (*DailyReport, error) {
	// 1. 拉取当天所有消息
	var messages []GroupMessage
	err := a.db.Where("group_id = ? AND DATE(created_at) = ?", groupID, date).
		Order("created_at ASC").
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	report := &DailyReport{
		GroupID:  groupID,
		Date:     date,
		TotalMsg: len(messages),
	}

	// 2. 按用户分组（在内存里做，避免多次查库）
	userMap := a.groupByUser(messages)

	// 3. 计算每个用户的统计数据
	stats := a.calcUserStats(userMap)

	// 4. 按发言数降序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].MsgCount > stats[j].MsgCount
	})

	report.UserStats = stats
	report.ActiveUsers = len(stats)

	// 5. 小时活跃度
	report.HourStats = a.calcHourStats(messages)

	// 6. 全群关键词（把所有人的发言合并后提取）
	allContents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.MsgType == "text" && msg.Content != "" {
			allContents = append(allContents, msg.Content)
		}
	}
	report.TopKeywords = ExtractKeywords(allContents, 8)

	return report, nil
}

// groupByUser 按userID把消息分桶，返回 map[userID][]消息
func (a *Aggregator) groupByUser(messages []GroupMessage) map[int64][]GroupMessage {
	m := make(map[int64][]GroupMessage)
	for _, msg := range messages {
		m[msg.UserID] = append(m[msg.UserID], msg)
	}
	return m
}

// calcUserStats 对每个用户的消息列表计算统计项
func (a *Aggregator) calcUserStats(userMap map[int64][]GroupMessage) []UserStat {
	stats := make([]UserStat, 0, len(userMap))

	for userID, msgs := range userMap {
		stat := UserStat{
			UserID:   userID,
			Nickname: msgs[0].Nickname, // 取今天第一条记录的昵称
		}

		textContents := make([]string, 0)

		for _, msg := range msgs {
			stat.MsgCount++

			switch msg.MsgType {
			case "image":
				stat.ImageCount++
			case "at":
				stat.AtCount++
			case "poke":
				stat.PokeCount++
			case "text", "mixed":
				if msg.Content != "" {
					textContents = append(textContents, msg.Content)
					// 短句判断：纯文字且5字以内
					if runeLen(msg.Content) <= 5 {
						stat.ShortCount++
					}
				}
			}

			// 凌晨判断
			if msg.Hour >= 0 && msg.Hour <= 4 {
				stat.NightOwl = true
			}
		}

		// 首末发言时段
		stat.FirstHour = msgs[0].Hour
		stat.LastHour = msgs[len(msgs)-1].Hour

		stat.AllContents = textContents

		// 筛选代表发言（见sampler.go）
		stat.SampleMsgs = SampleMessages(textContents)

		stats = append(stats, stat)
	}

	return stats
}

// calcHourStats 统计每小时消息量，返回Top5活跃时段
func (a *Aggregator) calcHourStats(messages []GroupMessage) []HourStat {
	hourCount := make(map[int]int)
	for _, msg := range messages {
		hourCount[msg.Hour]++
	}

	stats := make([]HourStat, 0, len(hourCount))
	for h, c := range hourCount {
		stats = append(stats, HourStat{Hour: h, Count: c})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})

	// 只取Top5
	if len(stats) > 5 {
		stats = stats[:5]
	}
	return stats
}

func runeLen(s string) int {
	return len([]rune(s))
}
