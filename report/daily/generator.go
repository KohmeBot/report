package daily

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gorm.io/gorm"
	"maps"
	"math/rand"
	"slices"
	"strings"
	"time"
)

type Report struct {
	Text  string
	Image []byte
}

type Generator struct {
	db         *gorm.DB
	invoker    *chataisdk.ChatAIInvoker
	env        plugin.Env
	chromeAddr string
}

func NewGenerator(env plugin.Env, db *gorm.DB, invoker *chataisdk.ChatAIInvoker, chromeAddr string) *Generator {
	return &Generator{
		env:        env,
		db:         db,
		invoker:    invoker,
		chromeAddr: chromeAddr,
	}
}

func (g *Generator) botNickName() string {
	//  NICKNAME
	if len(zero.BotConfig.NickName) > 0 {
		return zero.BotConfig.NickName[0]
	}
	return "bot"
}

func (g *Generator) BuildPrompt(group int64, t time.Time) (string, *DailyReport, error) {
	date := t.Format("2006-01-02")

	// 生成日报
	aggregator := NewAggregator(g.db)
	report, err := aggregator.Aggregate(group, date)
	if err != nil {
		return "", nil, fmt.Errorf("聚合失败: %w", err)
	}
	if report == nil {
		logrus.Infof("%d 昨日暂无数据", group)
		return "", nil, nil
	}

	data := g.buildPrompt(report)
	return data, report, nil
}

func (g *Generator) GenerateReport(group int64, t time.Time, theme *DailyTheme) (Report, error) {
	date := t.Format("2006-01-02")

	data, report, err := g.BuildPrompt(group, t)

	req := fmt.Sprintf(reportPrompt,
		theme.String(),
		data,
	)

	largeModel, err := g.invoker.NewModel(systemPrompt, true, false, true)
	if err != nil {
		return Report{}, err
	}

	res, err := g.invoker.DoRequestWithModel(req, largeModel)
	if err != nil {
		return Report{}, fmt.Errorf("AI调用失败: %w", err)
	}

	var reportRes ReportJSON
	err = json.Unmarshal([]byte(res), &reportRes)
	if err != nil {
		return Report{}, fmt.Errorf("JSON解析失败: %w", err)
	}

	dataJSON, _ := json.Marshal(report)
	themeJSON, _ := json.Marshal(theme)
	if err := g.saveReport(group, date, string(dataJSON), res, string(themeJSON)); err != nil {
		logrus.Warnf("持久化失败: %v", err)
	}

	r := newReportTemplateData(t, theme, reportRes, g.botNickName())

	imgBytes, err := r.renderReportImage(g.chromeAddr, group)
	if err != nil {
		logrus.Errorf("图片生成失败: %v", err)
	}

	return Report{
		Text:  reportRes.String(theme),
		Image: imgBytes,
	}, nil
}

func (g *Generator) saveReport(group int64, date, data, report, theme string) error {
	var stat GroupDailyStat
	err := g.db.Where("group_id = ? AND date = ?", group, date).First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return g.db.Create(&GroupDailyStat{
			GroupID: group,
			Date:    date,
			Data:    data,
			Report:  report,
			Theme:   theme,
		}).Error
	}
	return g.db.Model(&stat).Updates(map[string]any{
		"data":   data,
		"report": report,
		"theme":  theme,
	}).Error
}

// GetTodayTheme 查是否已生成过主题，有则直接复用
func (g *Generator) GetTodayTheme(t time.Time) (*DailyTheme, error) {
	var stat GroupDailyStat
	err := g.db.Where("date = ? AND theme != ''", t.Format("2006-01-02")).
		First(&stat).Error
	if err != nil {
		return nil, err
	}
	var theme DailyTheme
	if err := json.Unmarshal([]byte(stat.Theme), &theme); err != nil {
		return nil, err
	}
	return &theme, nil
}

func (g *Generator) GetUsedTheme(ts ...time.Time) ([]*DailyTheme, error) {
	dates := make([]string, 0, len(ts))
	for _, t := range ts {
		dates = append(dates, t.Format("2006-01-02"))
	}
	var stats []GroupDailyStat
	err := g.db.Where("date IN ?", dates).Find(&stats).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	res := make([]*DailyTheme, 0, len(stats))
	for _, stat := range stats {
		var theme DailyTheme
		_ = json.Unmarshal([]byte(stat.Theme), &theme)
		res = append(res, &theme)
	}
	return res, nil
}

func (g *Generator) GenerateTheme(t time.Time, exclude ...*DailyTheme) (*DailyTheme, error) {
	excludeTheme := make(map[string]*DailyTheme, len(exclude))
	// 做个去重
	for _, theme := range exclude {
		excludeTheme[theme.Theme] = theme
	}
	excludeStr := strings.Join(slices.Collect(maps.Keys(excludeTheme)), ",")

	weekdays := []string{"日", "一", "二", "三", "四", "五", "六"}
	req := fmt.Sprintf(themePrompt,
		t.Format("2006-01-02"),
		weekdays[t.Weekday()],
		excludeStr,
	)

	largeModel, err := g.invoker.NewModel(systemPrompt, true, false, true)
	if err != nil {
		return nil, err
	}

	res, err := g.invoker.DoRequestWithModel(req, largeModel)
	if err != nil {
		return nil, err
	}

	// 容错：AI可能在JSON外面加```json```
	res = strings.TrimSpace(res)
	res = strings.TrimPrefix(res, "```json")
	res = strings.TrimPrefix(res, "```")
	res = strings.TrimSuffix(res, "```")

	var theme DailyTheme
	if err := json.Unmarshal([]byte(strings.TrimSpace(res)), &theme); err != nil {
		return nil, fmt.Errorf("主题解析失败: %w, raw: %s", err, res)
	}
	return &theme, nil
}

// buildPrompt 把DailyReport拼成喂给AI的结构化文本
func (g *Generator) buildPrompt(r *DailyReport) string {
	var sb strings.Builder

	// 基本信息
	sb.WriteString(fmt.Sprintf("=== %s 群聊日报数据(%s - %s) ===\n\n", r.Date, formatTime(r.StartTime), formatTime(r.EndTime)))
	sb.WriteString(fmt.Sprintf("【基本数据】\n今日发言人数：%d人\n今日总消息数：%d条\n\n",
		r.ActiveUsers, r.TotalMsg))

	// 活跃时段
	sb.WriteString("【活跃时段Top5】\n")
	limit := len(r.TimeStats)
	if limit > 5 {
		limit = 5
	}
	for i, h := range r.TimeStats[:limit] {
		sb.WriteString(fmt.Sprintf("  %d. %s —— %d条\n", i+1, formatTime(h.Time), h.Count))
	}

	// 找出最冷清时段（0条发言的时间）
	silentTimes := findSilentTimes(r.StartTime, r.EndTime, r.TimeStats)
	if len(silentTimes) > 0 {
		sb.WriteString(fmt.Sprintf("群沉默时段：%s\n", formatTimes(silentTimes)))
	}
	sb.WriteString("\n")

	// 群友排行（只取前8，太多token爆炸）
	limit = len(r.UserStats)
	if limit > 8 {
		limit = 8
	}
	sb.WriteString(fmt.Sprintf("【发言数前%d群友数据】\n", limit))
	for i, stat := range r.UserStats[:limit] {
		sb.WriteString(g.buildUserBlock(i+1, stat))
	}

	// 潜水（发言<=2条的人）
	ghosts := []string{}
	for _, stat := range r.UserStats {
		if stat.MsgCount <= 2 {
			ghosts = append(ghosts, fmt.Sprintf("%s(%d条)", stat.Nickname, stat.MsgCount))
		}
	}
	if len(ghosts) > 0 {
		sb.WriteString(fmt.Sprintf("\n【今日边缘人】\n%s\n", strings.Join(ghosts, " / ")))
	}

	// 关键词
	if len(r.TopKeywords) > 0 {
		sb.WriteString("\n【今日高频词】\n")
		parts := make([]string, 0, len(r.TopKeywords))
		for _, kw := range r.TopKeywords {
			parts = append(parts, fmt.Sprintf("%s(%d次)", kw.Word, kw.Count))
		}
		sb.WriteString(strings.Join(parts, " / ") + "\n")
	}

	return sb.String()
}

func (g *Generator) buildUserBlock(rank int, stat UserStat) string {
	var sb strings.Builder

	// 基础行
	sb.WriteString(fmt.Sprintf("%d. %s｜发言%d条", rank, stat.Nickname, stat.MsgCount))

	// 时间跨度（一行，让AI自己判断有没有梗）
	if !stat.FirstTime.IsZero() && !stat.LastTime.IsZero() {

		sb.WriteString(fmt.Sprintf("｜第一条发言于%s 最后发言于%s",
			formatTime(stat.FirstTime),
			formatTime(stat.LastTime)))
		if stat.NightOwl {
			sb.WriteString("｜有凌晨发言")
		}
	}
	sb.WriteString("\n")

	// 消息类型分布（只列出>0的）
	typeDesc := []string{}
	for typ, n := range stat.MsgTypeCount {
		if n > 0 {
			typeDesc = append(typeDesc, fmt.Sprintf("%s%d", MsgTypeString(typ), n))
		}
	}

	if len(typeDesc) > 0 {
		sb.WriteString(fmt.Sprintf("   发言类型和数量：%s\n", strings.Join(typeDesc, "/")))
	}

	// 文字特征（纯数字，不加评语）
	textStats := []string{}
	if stat.MsgCount > 0 {
		textStats = append(textStats, fmt.Sprintf("短句率%d%%", stat.ShortCount*100/stat.MsgCount))
	}
	if stat.AvgMsgLen > 0 {
		textStats = append(textStats, fmt.Sprintf("均长%d字", stat.AvgMsgLen))
	}
	if stat.VocabSize > 0 {
		textStats = append(textStats, fmt.Sprintf("词汇量%d", stat.VocabSize))
	}
	if stat.ExclamCount > 0 {
		textStats = append(textStats, fmt.Sprintf("感叹号%d", stat.ExclamCount))
	}
	if stat.QuestionCount > 0 {
		textStats = append(textStats, fmt.Sprintf("问号%d", stat.QuestionCount))
	}
	if stat.EllipsisCount > 0 {
		textStats = append(textStats, fmt.Sprintf("省略号%d", stat.EllipsisCount))
	}
	if stat.LonelyCount > 0 && stat.MsgCount > 0 {
		textStats = append(textStats, fmt.Sprintf("无人回应%d条(%d%%)", stat.LonelyCount, stat.LonelyCount*100/stat.MsgCount))
	}
	if len(textStats) > 0 {
		sb.WriteString(fmt.Sprintf("   文字：%s\n", strings.Join(textStats, "/")))
	}

	// 发言节奏
	rhythm := stat.Rhythm
	rhythmStats := []string{}
	if rhythm.AvgInterval > 0 {
		rhythmStats = append(rhythmStats, fmt.Sprintf("均间隔%d分钟", int(rhythm.AvgInterval.Minutes())))
	}
	if rhythm.LongestSilence > 0 {
		rhythmStats = append(rhythmStats, fmt.Sprintf("最长沉默%d分钟", int(rhythm.LongestSilence.Minutes())))
	}
	if rhythm.BurstCount > 0 {
		rhythmStats = append(rhythmStats, fmt.Sprintf("连续发言%d次(最多单次%d条)", rhythm.BurstCount, rhythm.BurstMaxSize))
	}
	if rhythm.ActivePeriods > 1 {
		rhythmStats = append(rhythmStats, fmt.Sprintf("分%d个时间段活跃", rhythm.ActivePeriods))
	}
	if len(rhythmStats) > 0 {
		sb.WriteString(fmt.Sprintf("   节奏：%s\n", strings.Join(rhythmStats, "/")))
	}

	// 复读（有就列出来，让AI判断梗点）
	if stat.RepeatCount >= 2 {
		preview := stat.RepeatMsg
		if runeLen(preview) > 15 {
			preview = string([]rune(preview)[:15]) + "..."
		}
		sb.WriteString(fmt.Sprintf("   复读：「%s」×%d次\n", preview, stat.RepeatCount))
	}

	// 互动数据（精简，只列关键数字）
	totalOut := totalCount(stat.InteractionCount)
	totalIn := totalCount(stat.BeReplied)
	uniqueOut := len(stat.InteractionCount)
	uniqueIn := len(stat.BeReplied)

	if totalOut+totalIn > 0 {
		sb.WriteString(fmt.Sprintf("   互动：主动%d次(%d人)/被动%d次(%d人)\n",
			totalOut, uniqueOut, totalIn, uniqueIn))
	}

	// 最强互动对（双向都列出来，让AI判断是CP还是单押）
	topOut, topOutCount := topUser(stat.InteractionCount)
	topIn, topInCount := topUser(stat.BeReplied)
	if topOutCount >= 2 {
		inBack := stat.BeReplied[topOut]
		sb.WriteString(fmt.Sprintf("   最常找：%s（%d次，对方回找%d次）\n",
			topOut.Nickname, topOutCount, inBack))
	}
	if topInCount >= 2 && topIn != topOut {
		outBack := stat.InteractionCount[topIn]
		sb.WriteString(fmt.Sprintf("   最常被找：%s（%d次，自己回找%d次）\n",
			topIn.Nickname, topInCount, outBack))
	}

	// 被互动但完全不回应的人数
	ignoredCount := 0
	for user, inMsgs := range stat.BeRepliedMessage {
		if len(inMsgs) >= 3 && stat.InteractionCount[user] == 0 {
			ignoredCount++
		}
	}
	if ignoredCount > 0 {
		sb.WriteString(fmt.Sprintf("   已读不回：%d人\n", ignoredCount))
	}

	// 代表发言（上限4条，带时间）
	if len(stat.SampleMsgs) > 0 {
		sb.WriteString("   代表发言：\n")
		for _, m := range stat.SampleMsgs {
			msg := m.Content
			r := []rune(msg)
			if len(r) > 40 {
				msg = string(r[:40]) + "..."
			}
			sb.WriteString(fmt.Sprintf("     [%s] 「%s」\n",
				formatTime(m.CreatedAt), msg))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// analyzeInteractionStyle 分析和某人互动时的说话风格
func analyzeInteractionStyle(msgs []GroupMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	questionCount := 0
	exclamCount := 0
	shortCount := 0
	totalLen := 0

	for _, msg := range msgs {
		if msg.MsgType != MsgTypeText {
			continue
		}
		questionCount += strings.Count(msg.Content, "?") + strings.Count(msg.Content, "？")
		exclamCount += strings.Count(msg.Content, "!") + strings.Count(msg.Content, "！")
		if runeLen(msg.Content) <= 5 {
			shortCount++
		}
		totalLen += runeLen(msg.Content)
	}

	avgLen := 0
	if len(msgs) > 0 {
		avgLen = totalLen / len(msgs)
	}

	// 按优先级匹配，只返回最突出的一个
	switch {
	case questionCount >= len(msgs)/2:
		return "连环发问型，问题比答案多"
	case exclamCount >= len(msgs)/2:
		return "情绪输出型，感叹号停不下来"
	case shortCount >= len(msgs)*7/10 && avgLen <= 4:
		return "惜字如金型，三个字以内解决一切"
	case avgLen >= 30:
		return "长篇大论型，每次回复都像在写作文"
	default:
		return ""
	}
}

// topUser 找map里value最大的key
func topUser(m map[User]int) (User, int) {
	var top User
	maxCount := 0
	for u, c := range m {
		if c > maxCount {
			maxCount = c
			top = u
		}
	}
	return top, maxCount
}

// totalCount 求map所有value的和
func totalCount(m map[User]int) int {
	total := 0
	for _, c := range m {
		total += c
	}
	return total
}

func findSilentTimes(start, end time.Time, timeStats []TimeStat) []time.Time {
	if end.Before(start) {
		return nil
	}

	// 已有发言时间
	active := make(map[time.Time]struct{}, len(timeStats))
	for _, stat := range timeStats {
		if stat.Count > 0 {
			active[stat.Time] = struct{}{}
		}
	}

	var silent []time.Time

	// 按小时检查
	for t := start; !t.After(end); t = t.Add(time.Hour) {
		if _, ok := active[t]; !ok {
			silent = append(silent, t)
		}
	}

	return silent
}

func formatTimes(times []time.Time) string {
	parts := make([]string, len(times))
	for i, t := range times {
		parts[i] = fmt.Sprintf("%s", formatTime(t))
	}
	if len(parts) > 5 {
		parts = parts[:5]
		return strings.Join(parts, "、") + "等"
	}
	return strings.Join(parts, "、")
}

// FallbackTheme 主题生成失败时的兜底
func FallbackTheme() *DailyTheme {
	return themes[rand.Intn(len(themes))]
}

var themes = []*DailyTheme{
	{
		Theme:             "黑暗之魂",
		Role:              "火祭司",
		Style:             "死亡提示语风格，冷静克制，每句话都在暗示活着没有意义，大量使用「……已死」「获得了XX魂」「篝火已熄灭」",
		UserFormat:        "用{nickname}的昨日行为判定其死亡原因和获得的魂数量",
		GhostFormat:       "这些人已空洞化，失去了点击屏幕的欲望，灵魂在某处徘徊",
		MvpHeader:         "💀 死亡档案",
		MomentHeader:      "⚡ 历史铭刻之时",
		MomentFormat:      "用篝火燃烧烈度描述这段时间，说明当时发生了什么集体死亡事件",
		InteractionHeader: "🕸️ 誓约与背叛",
		InteractionFormat: "{from}持续向{to}发动侵入，使用了……",
		TriviaHeader:      "🎲 隐藏属性揭示",
		TriviaFormat:      "用装备词条的形式揭示这个反直觉的数据，格式像暗魂的武器说明",
		DiagnosisHeader:   "🌡️ 世界褪色程度",
		GhostHeader:       "👻 空洞化名单",
		Visual: ThemeVisual{
			BgColor:         "#0d0d0d",
			TextColor:       "#c8b89a",
			AccentColor:     "#ff6b35",
			HeaderColor:     "#1a1a1a",
			FontStyle:       "normal",
			BorderStyle:     "glow",
			EmojiDecoration: "💀🔥",
		},
	},
	{
		Theme:             "碧蓝档案",
		Role:              "基沃托斯联邦调查部老师",
		Style:             "学校报告文风，用社团/部活/校规框架描述群友行为，老师视角带着无奈和宠溺，常用「老师表示」「已记入档案」「申请紧急镇压」",
		UserFormat:        "以{nickname}的社团活动报告形式点评，说明其昨日违规行为及处分建议",
		GhostFormat:       "以下学生昨日无故旷课，已通知家长，正在联合对策委员会展开搜寻",
		MvpHeader:         "📋 问题学生档案",
		MomentHeader:      "⚡ 事件发生经过",
		MomentFormat:      "用学校紧急事件报告的格式描述这段时间，说明老师是否申请了镇压",
		InteractionHeader: "🕸️ 羁绊关系调查",
		InteractionFormat: "据报告{from}持续对{to}发动社交攻势，联邦调查部正在评估是否立案",
		TriviaHeader:      "🎲 老师的意外发现",
		TriviaFormat:      "用老师批改作业时的语气揭示这个数据，结尾加一句无奈感叹",
		DiagnosisHeader:   "🌡️ 基沃托斯今日现状",
		GhostHeader:       "👻 旷课名单",
		Visual: ThemeVisual{
			BgColor:         "#f0f7ff",
			TextColor:       "#2c3e50",
			AccentColor:     "#4a9eff",
			HeaderColor:     "#ddeeff",
			FontStyle:       "normal",
			BorderStyle:     "solid",
			EmojiDecoration: "📋✨",
		},
	},
	{
		Theme:             "JOJO奇妙冒险",
		Role:              "替身能力鉴定师",
		Style:             "替身能力说明书风格，所有行为都被解读为替身能力，大量使用「能力名称」「射程」「破坏力」「精密动作性」「持续力」「成长性」六维评分，语气夸张中二",
		UserFormat:        "为{nickname}的昨日行为命名一个替身，给出能力说明和六维评分",
		GhostFormat:       "以下替身使用者已进入时间停止状态，推测遭遇了ザ・ワールド",
		MvpHeader:         "🌟 替身能力鉴定书",
		MomentHeader:      "⚡ 能力爆发时刻",
		MomentFormat:      "用替身能力集中爆发的角度描述这段时间，说明当时群聊空间发生了什么扭曲",
		InteractionHeader: "🕸️ 替身对决记录",
		InteractionFormat: "{from}的替身持续向{to}发动近距离攻击，对决结果……",
		TriviaHeader:      "🎲 隐藏能力揭示",
		TriviaFormat:      "用「实际上这个能力还有一个隐藏效果」的格式揭示这个反直觉数据",
		DiagnosisHeader:   "🌡️ 群聊空间扭曲程度",
		GhostHeader:       "👻 时间停止名单",
		Visual: ThemeVisual{
			BgColor:         "#1a0a2e",
			TextColor:       "#e8d5ff",
			AccentColor:     "#c0a0ff",
			HeaderColor:     "#2d1052",
			FontStyle:       "bold",
			BorderStyle:     "double",
			EmojiDecoration: "🌟⭐",
		},
	},
}
