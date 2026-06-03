package daily

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Aggregator struct {
	db       *gorm.DB
	invoker  *chataisdk.ChatAIInvoker
	thinking bool
}

func NewAggregator(db *gorm.DB, invoker *chataisdk.ChatAIInvoker, thinking bool) *Aggregator {
	return &Aggregator{db: db, invoker: invoker, thinking: thinking}
}

// Aggregate 对指定群、指定日期做全量聚合，返回DailyReport
func (a *Aggregator) Aggregate(groupID int64, date time.Time, duration time.Duration) (*AggregateData, map[int64]User, error) {
	now := time.Now()
	defer func() {
		latency := time.Since(now)
		logrus.Infof("AggregateData %d %s 生成完毕，耗时 %s", groupID, date, latency)
	}()

	// 群聊日从凌晨4点开始
	start := time.Date(
		date.Year(),
		date.Month(),
		date.Day(),
		4, 0, 0, 0,
		time.Local,
	)

	end := start.Add(duration)

	// 1. 拉取当天所有消息
	var messages []GroupMessage
	err := a.db.
		Where(
			"group_id = ? AND created_at >= ? AND created_at < ?",
			groupID,
			start,
			end,
		).
		Order("created_at ASC").
		Find(&messages).Error
	if err != nil {
		return nil, nil, err
	}
	if len(messages) == 0 {
		return nil, nil, nil
	}

	// message 按createAt升序
	slices.SortFunc(messages, func(a, b GroupMessage) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	report := &AggregateData{
		GroupMessages: messages,
		GroupID:       groupID,
		Date:          date,
		TotalMsg:      len(messages),
		StartTime:     start,
		EndTime:       end,
	}

	// 2. 按用户分组（在内存里做，避免多次查库）
	userMap := a.groupByUser(messages)

	// 把用户ID映射为用户
	ump := make(map[int64]User)
	for user := range userMap {
		ump[user.UserId] = user
	}

	// 3. 计算每个用户的统计数据
	stats := a.calcUserStats(userMap, ump, messages)

	// 4. 按发言数降序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].MsgCount > stats[j].MsgCount
	})

	report.UserStats = stats
	report.ActiveUsers = len(stats)

	// 5. 小时活跃度
	report.TimeStats = a.calcTimeStats(messages, start, end)

	// 6. 字符数量与表情包数量
	for _, msg := range messages {
		if msg.MsgType == MsgTypeImg {
			report.TotalMemeCount++
		}
		report.TotalCharCount += runeLen(msg.Content)
	}

	// 7. 截取首条和最后一条消息
	report.FirstMessage = messages[0]
	report.EndMessage = messages[len(messages)-1]

	// 8. 摘取热点信息
	report.HotPeriod = findHotSegment(messages)

	// 9. 摘取被复读最多次的消息
	report.RepeatMessage = findGroupRepeatMsg(messages, ump)

	return report, ump, nil
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
func (a *Aggregator) calcUserStats(userMap map[User][]GroupMessage, ump map[int64]User, allGroupMsgs []GroupMessage) []UserStat {
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

	stats := make([]UserStat, 0, len(userMap))

	for u, msgs := range userMap {
		stat := UserStat{
			User:               u,
			MsgTypeCount:       map[string]int{},
			InteractionCount:   map[User]int{},
			InteractionMessage: map[User][]GroupMessage{},
			BeReplied:          map[User]int{},
			BeRepliedMessage:   map[User][]GroupMessage{},
			FirstMessage:       msgs[0],
			EndMessage:         msgs[len(msgs)-1],
		}

		textContents := make([]string, 0)

		// ---------- 逐条消息遍历 ----------
		for _, msg := range msgs {
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

		// 统计发言节奏
		stat.Rhythm = calcRhythm(msgs)

		// BeReplied：群里的 reply at 类型消息里，targetID == 当前用户的数量
		for _, gm := range allGroupMsgs {
			sendUser, ok := ump[gm.UserID] // 发言人
			if !ok {
				continue
			}
			if gm.TargetUserID == u.UserId {
				stat.BeReplied[sendUser]++
				stat.BeRepliedMessage[sendUser] = append(stat.BeRepliedMessage[sendUser], gm)
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

		stat.AllContents = textContents

		stats = append(stats, stat)
	}

	return stats
}

func calcRhythm(msgs []GroupMessage) RhythmStat {
	if len(msgs) <= 1 {
		return RhythmStat{}
	}

	stat := RhythmStat{}
	totalInterval := time.Duration(0)

	// 计算间隔
	intervals := make([]time.Duration, 0, len(msgs)-1)
	for i := 1; i < len(msgs); i++ {
		gap := msgs[i].CreatedAt.Sub(msgs[i-1].CreatedAt)
		intervals = append(intervals, gap)
		totalInterval += gap
		if gap > stat.LongestSilence {
			stat.LongestSilence = gap
		}
	}

	stat.AvgInterval = totalInterval / time.Duration(len(intervals))

	// 活跃时间段：间隔超过30分钟算断开
	stat.ActivePeriods = 1
	for _, gap := range intervals {
		if gap > 30*time.Minute {
			stat.ActivePeriods++
		}
	}

	// 爆发检测：滑动窗口，5分钟内发了5条以上
	for i := 0; i < len(msgs); i++ {
		burstSize := 1
		for j := i + 1; j < len(msgs); j++ {
			if msgs[j].CreatedAt.Sub(msgs[i].CreatedAt) <= 5*time.Minute {
				burstSize++
			} else {
				break
			}
		}
		if burstSize >= 5 {
			stat.BurstCount++
			if burstSize > stat.BurstMaxSize {
				stat.BurstMaxSize = burstSize
			}
			// 跳过这次爆发已统计的消息
			i += burstSize - 1
		}
	}

	return stat
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

func findGroupRepeatMsg(msgs []GroupMessage, ump map[int64]User) RepeatStat {
	var textMsgs []GroupMessage
	for _, msg := range msgs {
		if msg.MsgType == MsgTypeText && msg.Content != "" {
			textMsgs = append(textMsgs, msg)
		}
	}
	if len(textMsgs) == 0 {
		return RepeatStat{}
	}

	// key: content, value: 合并后的统计
	merged := make(map[string]*RepeatStat)

	cur := &RepeatStat{
		FirstSender: ump[textMsgs[0].UserID],
		StartTime:   textMsgs[0].CreatedAt,
	}
	curContent := textMsgs[0].Content
	cur.Count = 1

	flush := func() {
		if cur.Count < 3 {
			return
		}
		if existing, ok := merged[curContent]; ok {
			// 合并：累加次数，保留最早的发起人和时间
			existing.Count += cur.Count
			if cur.StartTime.Before(existing.StartTime) {
				existing.FirstSender = cur.FirstSender
				existing.StartTime = cur.StartTime
			}
		} else {
			merged[curContent] = &RepeatStat{
				Count:       cur.Count,
				FirstSender: cur.FirstSender,
				StartTime:   cur.StartTime,
			}
		}
	}

	for i := 1; i < len(textMsgs); i++ {
		msg := textMsgs[i]
		if msg.Content == curContent {
			cur.Count++
		} else {
			flush()
			curContent = msg.Content
			cur = &RepeatStat{
				Count:       1,
				FirstSender: ump[msg.UserID],
				StartTime:   msg.CreatedAt,
			}
		}
	}
	flush()

	if len(merged) == 0 {
		return RepeatStat{}
	}

	// 找总次数最多的
	var best RepeatStat
	for content, seg := range merged {
		if seg.Count > best.Count {
			best = RepeatStat{
				Content:     content,
				Count:       seg.Count,
				FirstSender: seg.FirstSender,
				StartTime:   seg.StartTime,
			}
		}
	}

	return best
}

// calcTimeStats 统计时间消息量，返回时段
func (a *Aggregator) calcTimeStats(messages []GroupMessage, start time.Time, end time.Time) []TimeStat {
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

	// 补全 start~end 范围内缺失的小时，count 为 0
	startHour := start.Truncate(time.Hour)
	endHour := end.Truncate(time.Hour)
	for t := startHour; !t.After(endHour); t = t.Add(time.Hour) {
		if _, ok := timeCount[t]; !ok {
			timeCount[t] = 0
		}
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
