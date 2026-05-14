package daily

import (
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"slices"
	"sort"
	"strings"
	"time"
)

type Aggregator struct {
	db *gorm.DB
}

func NewAggregator(db *gorm.DB) *Aggregator {
	return &Aggregator{db: db}
}

// Aggregate 对指定群、指定日期做全量聚合，返回DailyReport
func (a *Aggregator) Aggregate(groupID int64, date string) (*DailyReport, error) {
	now := time.Now()
	defer func() {
		latency := time.Since(now)
		logrus.Infof("DailyReport %d %s 生成完毕，耗时 %s", groupID, date, latency)
	}()

	startDay, err := time.ParseInLocation(
		"2006-01-02",
		date,
		time.Local,
	)
	if err != nil {
		return nil, err
	}

	// 群聊日从凌晨4点开始
	start := time.Date(
		startDay.Year(),
		startDay.Month(),
		startDay.Day(),
		4, 0, 0, 0,
		time.Local,
	)

	end := start.Add(24 * time.Hour)

	// 1. 拉取当天所有消息
	var messages []GroupMessage
	err = a.db.
		Where(
			"group_id = ? AND created_at >= ? AND created_at < ?",
			groupID,
			start,
			end,
		).
		Order("created_at ASC").
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	// message 按createAt升序
	slices.SortFunc(messages, func(a, b GroupMessage) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	report := &DailyReport{
		GroupID:  groupID,
		Date:     date,
		TotalMsg: len(messages),
	}

	// 2. 按用户分组（在内存里做，避免多次查库）
	userMap := a.groupByUser(messages)

	// 3. 计算每个用户的统计数据
	stats := a.calcUserStats(userMap, messages)

	// 4. 按发言数降序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].MsgCount > stats[j].MsgCount
	})

	report.UserStats = stats
	report.ActiveUsers = len(stats)

	// 5. 小时活跃度
	report.TimeStats = a.calcTimeStats(messages)

	// 6. 全群关键词（把所有人的发言合并后提取）
	allContents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.MsgType == MsgTypeText && msg.Content != "" {
			allContents = append(allContents, msg.Content)
		}
	}
	report.TopKeywords = ExtractKeywords(allContents, 8)

	return report, nil
}

// groupByUser 按userID把消息分桶，返回 map[userID][]消息
func (a *Aggregator) groupByUser(messages []GroupMessage) map[User][]GroupMessage {
	m := make(map[int64][]GroupMessage)
	for _, msg := range messages {
		// userId是唯一的，先用userId做一次聚合
		m[msg.UserID] = append(m[msg.UserID], msg)
	}

	res := make(map[User][]GroupMessage)
	for uid, msgs := range m {
		// 直接用最新的昵称
		u := User{
			UserId:   uid,
			Nickname: msgs[len(msgs)-1].Nickname,
		}
		res[u] = msgs
	}

	return res
}

// calcUserStats 对每个用户的消息列表计算统计项
func (a *Aggregator) calcUserStats(userMap map[User][]GroupMessage, allGroupMsgs []GroupMessage) []UserStat {
	// 预处理：建一个群消息时间索引，用于计算 LonelyCount 和 BeReplied
	// key: 消息ID，value: 下一条其他人消息距离的秒数
	type msgMeta struct {
		userID    int64
		timestamp time.Time
	}
	groupTimeline := make([]msgMeta, 0, len(allGroupMsgs))
	for _, m := range allGroupMsgs {
		groupTimeline = append(groupTimeline, msgMeta{m.UserID, m.CreatedAt})
	}

	// 把用户ID映射为用户
	ump := make(map[int64]User)
	for user := range userMap {
		ump[user.UserId] = user
	}

	stats := make([]UserStat, 0, len(userMap))

	for u, msgs := range userMap {
		stat := UserStat{
			User:               u,
			MsgTypeCount:       map[string]int{},
			InteractionCount:   map[User]int{},
			InteractionMessage: map[User][]GroupMessage{},
			BeReplied:          map[User]int{},
			BeRepliedMessage:   map[User][]GroupMessage{},
		}

		textContents := make([]string, 0)

		// ---------- 逐条消息遍历 ----------
		for i, msg := range msgs {
			stat.MsgCount++
			stat.MsgTypeCount[msg.MsgType]++

			if msg.MsgType == MsgTypeText && msg.Content != "" {
				textContents = append(textContents, msg.Content)

				if runeLen(msg.Content) <= 5 {
					stat.ShortCount++
				}

				// 情绪统计
				stat.ExclamCount += strings.Count(msg.Content, "!") +
					strings.Count(msg.Content, "！")
				stat.QuestionCount += strings.Count(msg.Content, "?") +
					strings.Count(msg.Content, "？")
				stat.EllipsisCount += strings.Count(msg.Content, "……") +
					strings.Count(msg.Content, "...")
			}

			// 凌晨判断
			if msg.CreatedAt.Hour() >= 0 && msg.CreatedAt.Hour() <= 4 {
				stat.NightOwl = true
			}

			// 连发检测：当前消息往前看，60秒内自己连发了3条
			if i >= 2 {
				d := msgs[i].CreatedAt.Sub(msgs[i-2].CreatedAt)
				if d <= 60*time.Second {
					stat.BurstCount++
				}
			}
		}

		// ---------- 孤独指数 & 被回复数（扫群时间线）----------
		for _, msg := range msgs {

			// 统计回复的次数
			targetUser, ok := ump[msg.TargetUserID]
			if ok {
				stat.InteractionCount[targetUser]++
				stat.InteractionMessage[targetUser] = append(stat.InteractionMessage[targetUser], msg)
			}

			// 在群时间线里找这条消息之后5分钟内有没有其他人发言
			hasResponse := false
			for _, gm := range groupTimeline {
				if gm.userID == u.UserId {
					continue
				}
				diff := gm.timestamp.Sub(msg.CreatedAt)
				if diff > 0 && diff <= 5*time.Minute {
					hasResponse = true
					break
				}
				if diff > 5*time.Minute {
					break // 时间线有序，超过5分钟直接break
				}
			}
			if !hasResponse {
				stat.LonelyCount++
			}
		}

		// BeReplied：群里的 reply at 类型消息里，targetID == 当前用户的数量
		for _, gm := range allGroupMsgs {
			targetUser, has := ump[gm.TargetUserID]
			if !has {
				continue
			}
			if gm.TargetUserID == u.UserId {
				stat.BeReplied[targetUser]++
				stat.BeRepliedMessage[targetUser] = append(stat.BeRepliedMessage[targetUser], gm)
			}
		}

		// ---------- 词汇量 & 平均发言长度 ----------
		charSet := make(map[rune]bool)
		totalLen := 0
		for _, c := range textContents {
			totalLen += runeLen(c)
			for _, r := range c {
				if !strings.ContainsRune("，。！？,.!? \t哈呵嗯啊的了是", r) {
					charSet[r] = true
				}
			}
		}
		stat.VocabSize = len(charSet)
		if len(textContents) > 0 {
			stat.AvgMsgLen = totalLen / len(textContents)
		}

		// ---------- 复读检测 ----------
		stat.RepeatMsg, stat.RepeatCount = findRepeatMsg(textContents)

		// ---------- 首末发言时段 ----------
		stat.FirstTime = msgs[0].CreatedAt
		stat.LastTime = msgs[len(msgs)-1].CreatedAt

		stat.AllContents = textContents
		stat.SampleMsgs = SampleMessages(msgs)

		stats = append(stats, stat)
	}

	return stats
}

// findRepeatMsg 找出今天复读次数最多的那条发言
func findRepeatMsg(contents []string) (msg string, count int) {
	freq := make(map[string]int)
	for _, c := range contents {
		key := strings.ToLower(strings.TrimSpace(c))
		if runeLen(key) >= 2 {
			freq[key]++
		}
	}
	for k, v := range freq {
		if v > count {
			count = v
			msg = k
		}
	}
	if count < 2 {
		return "", 0
	}
	return
}

// calcTimeStats 统计时间消息量，返回时段
func (a *Aggregator) calcTimeStats(messages []GroupMessage) []TimeStat {
	timeCount := make(map[time.Time]int)
	for _, msg := range messages {
		t := time.Date(
			msg.CreatedAt.Year(),
			msg.CreatedAt.Month(),
			msg.CreatedAt.Day(),
			msg.CreatedAt.Hour(),
			0, 0, 0, time.Local)
		timeCount[t]++
	}

	stats := make([]TimeStat, 0, len(timeCount))
	for t, c := range timeCount {
		stats = append(stats, TimeStat{Time: t, Count: c})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})

	return stats
}

func runeLen(s string) int {
	return len([]rune(s))
}
