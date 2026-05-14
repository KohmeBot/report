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
	firstTime := r.TimeStats[0].Time
	lastTime := r.TimeStats[len(r.TimeStats)-1].Time
	sb.WriteString(fmt.Sprintf("=== %s 群聊日报数据(%s - %s) ===\n\n", r.Date, formatTime(firstTime), formatTime(lastTime)))
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
	silentTimes := findSilentTimes(r.TimeStats)
	if len(silentTimes) > 0 {
		sb.WriteString(fmt.Sprintf("群沉默时段：%s（大家都不在）\n", formatTimes(silentTimes)))
	}
	sb.WriteString("\n")

	// 群友排行（只取前8，太多token爆炸）
	sb.WriteString("【群友今日表现】\n")
	limit = len(r.UserStats)
	if limit > 8 {
		limit = 8
	}
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
	sb.WriteString(fmt.Sprintf("%d. %s —— 发言%d条\n", rank, stat.Nickname, stat.MsgCount))

	traits := []string{}

	// 短句率
	if stat.MsgCount > 0 {
		shortRate := stat.ShortCount * 100 / stat.MsgCount
		switch {
		case shortRate >= 80:
			traits = append(traits, fmt.Sprintf("发言%d%%是短句，惜字如金", shortRate))
		case shortRate >= 50:
			traits = append(traits, "说话偏短，能一个字绝不两个字")
		}
	}

	// 图片/表情包
	if stat.MsgCount > 0 {
		imageCount := stat.MsgTypeCount[MsgTypeImg]
		imgRate := imageCount * 100 / stat.MsgCount
		switch {
		case imgRate >= 60:
			traits = append(traits, fmt.Sprintf("发了%d张图占发言60%%+，靠图说话", imageCount))
		case imageCount >= 5:
			traits = append(traits, fmt.Sprintf("发了%d张图", imageCount))
		}
	}

	// 戳一戳
	pokeCount := stat.MsgTypeCount[MsgTypePoke]
	switch {
	case pokeCount >= 10:
		traits = append(traits, fmt.Sprintf("戳了别人%d次，戳戳狂魔", pokeCount))
	case pokeCount >= 3:
		traits = append(traits, fmt.Sprintf("戳了别人%d次", pokeCount))
	case pokeCount == 1:
		traits = append(traits, "戳了别人1次，不知道什么心态")
	}

	// at别人
	atCount := stat.MsgTypeCount[MsgTypeAt]
	switch {
	case atCount >= 10:
		traits = append(traits, fmt.Sprintf("@了别人%d次，群里的点名机器", atCount))
	case atCount >= 3:
		traits = append(traits, fmt.Sprintf("@了别人%d次", atCount))
	}

	// 转发消息(搬屎)
	forwardCount := stat.MsgTypeCount[MsgTypeForward]
	switch {
	case forwardCount >= 5:
		traits = append(traits, fmt.Sprintf("转发了%d条，今日搬屎冠军", forwardCount))
	case forwardCount >= 2:
		traits = append(traits, fmt.Sprintf("搬了%d坨屎(转发消息)过来，群友们谢谢你", forwardCount))
	case forwardCount == 1:
		traits = append(traits, "搬了一坨屎(转发消息)过来，就这一坨，但很关键")
	}

	// 语音消息
	recordCount := stat.MsgTypeCount[MsgTypeRecord]
	switch {
	case recordCount >= 5:
		traits = append(traits, fmt.Sprintf("发了%d条语音，打字是不会打字的", recordCount))
	case recordCount >= 2:
		traits = append(traits, fmt.Sprintf("发了%d条语音，懒得打字", recordCount))
	case recordCount == 1:
		traits = append(traits, "发了1条语音，不知道说了什么，反正没人听")
	}

	// 互动特征分析
	traits = append(traits, g.buildInteractionTraits(stat)...)

	// 孤独指数
	if stat.MsgCount > 0 {
		lonelyRate := stat.LonelyCount * 100 / stat.MsgCount
		switch {
		case lonelyRate >= 80:
			traits = append(traits, fmt.Sprintf(
				"%d条发言里%d%%发出后5分钟无人回应，今日最孤独", stat.MsgCount, lonelyRate))
		case lonelyRate >= 50:
			traits = append(traits, fmt.Sprintf("超过一半发言没人接，有点冷场"))
		}
	}

	// 连发
	switch {
	case stat.BurstCount >= 5:
		traits = append(traits, fmt.Sprintf("连发%d次，话痨", stat.BurstCount))
	case stat.BurstCount >= 2:
		traits = append(traits, fmt.Sprintf("有%d次60秒内连发3条以上", stat.BurstCount))
	}

	// 情绪
	if stat.ExclamCount >= 10 {
		traits = append(traits, fmt.Sprintf("用了%d个感叹号，今天情绪高涨", stat.ExclamCount))
	}
	if stat.QuestionCount >= 8 {
		traits = append(traits, fmt.Sprintf("问了%d个问号，疑似问题很多", stat.QuestionCount))
	}
	if stat.EllipsisCount >= 5 {
		traits = append(traits, fmt.Sprintf("用了%d个省略号，意味深长还是说不完整", stat.EllipsisCount))
	}

	// 词汇量 vs 发言量的矛盾
	if stat.MsgCount >= 10 && stat.VocabSize > 0 {
		switch {
		case stat.VocabSize < 20 && stat.MsgCount >= 15:
			traits = append(traits, fmt.Sprintf(
				"发了%d条但只用了%d种词，翻来覆去就那几个词", stat.MsgCount, stat.VocabSize))
		case stat.VocabSize >= 80:
			traits = append(traits, fmt.Sprintf("用词丰富，今天输出了%d种不同的词", stat.VocabSize))
		}
	}

	// 平均发言长度
	switch {
	case stat.AvgMsgLen >= 30:
		traits = append(traits, fmt.Sprintf("平均每条%d字，长篇大论型选手", stat.AvgMsgLen))
	case stat.AvgMsgLen <= 3 && stat.MsgCount >= 10:
		traits = append(traits, fmt.Sprintf("平均每条只有%d字，极简主义者", stat.AvgMsgLen))
	}

	// 复读
	if stat.RepeatCount >= 3 {
		preview := stat.RepeatMsg
		if runeLen(preview) > 15 {
			preview = string([]rune(preview)[:15]) + "..."
		}
		traits = append(traits, fmt.Sprintf(
			"今天把「%s」说了%d次，复读机", preview, stat.RepeatCount))
	}

	// 时间特征
	firstStr := formatTime(stat.FirstTime)
	lastStr := formatTime(stat.LastTime)
	activeSpan := stat.LastTime.Sub(stat.FirstTime)

	switch {
	case stat.NightOwl && stat.FirstTime.Hour() >= 22:
		traits = append(traits, fmt.Sprintf(
			"%s开始发言，%s还没睡，全程夜班", firstStr, lastStr))

	case stat.NightOwl:
		traits = append(traits, fmt.Sprintf(
			"凌晨还在发言，%s才消失", lastStr))

	case stat.FirstTime.Hour() <= 7 && !stat.LastTime.After(stat.FirstTime.Add(12*time.Hour)):
		traits = append(traits, fmt.Sprintf(
			"%s就开始发言，早鸟型，%s沉默", firstStr, lastStr))

	case activeSpan >= 14*time.Hour:
		traits = append(traits, fmt.Sprintf(
			"从%s聊到%s，跨度%d小时，全天候在线",
			firstStr, lastStr, int(activeSpan.Hours())))

	case activeSpan <= time.Hour && stat.MsgCount >= 10:
		traits = append(traits, fmt.Sprintf(
			"%d分钟内集中发了%d条，打完就跑",
			int(activeSpan.Minutes()), stat.MsgCount))
	}

	// 写入特征
	for _, t := range traits {
		sb.WriteString(fmt.Sprintf("   · %s\n", t))
	}

	// 代表发言
	if len(stat.SampleMsgs) > 0 {
		quoted := make([]string, 0, len(stat.SampleMsgs))
		for _, msg := range stat.SampleMsgs {
			r := []rune(msg)
			if len(r) > 40 {
				msg = string(r[:40]) + "..."
			}
			quoted = append(quoted, "「"+msg+"」")
		}
		sb.WriteString("   代表发言：" + strings.Join(quoted, " / ") + "\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

func (g *Generator) buildInteractionTraits(stat UserStat) []string {
	traits := []string{}

	totalOut := totalCount(stat.InteractionCount) // 主动互动总次数
	totalIn := totalCount(stat.BeReplied)         // 被互动总次数

	// ---- 1. 社交能量：主动 vs 被动 ----
	if totalOut+totalIn >= 5 {
		switch {
		case totalOut >= totalIn*3:
			traits = append(traits, fmt.Sprintf(
				"主动互动%d次，被互动%d次",
				totalOut, totalIn))
		case totalIn >= totalOut*3:
			traits = append(traits, fmt.Sprintf(
				"被互动%d次，只主动互动%d次",
				totalIn, totalOut))
		case totalOut >= 5 && totalIn >= 5:
			traits = append(traits, fmt.Sprintf(
				"主动互动%d次，被互动%d次", totalOut, totalIn))
		}
	}

	// ---- 2. 单押关系：有没有互动高度集中的对象 ----
	topOut, topOutCount := topUser(stat.InteractionCount)
	topIn, topInCount := topUser(stat.BeReplied)

	// 主动单押
	if totalOut > 0 && topOutCount*10 >= totalOut*7 && topOutCount >= 3 {
		traits = append(traits, fmt.Sprintf(
			"%d%%的互动都给了%s，不知道算痴情还是烦人",
			topOutCount*100/totalOut, topOut.Nickname))
	}

	// 被动单押：某人疯狂找你
	if totalIn > 0 && topInCount*10 >= totalIn*7 && topInCount >= 3 {
		traits = append(traits, fmt.Sprintf(
			"被%s互动%d次，占被互动总量%d%%",
			topIn.Nickname, topInCount, topInCount*100/totalIn))
	}

	// ---- 3. 两人互相疯狂找对方 ----
	for user, outCount := range stat.InteractionCount {
		inCount := stat.BeReplied[user]
		// 双方互动都超过5次，且都占各自互动量的50%以上
		if outCount >= 5 && inCount >= 5 &&
			outCount*10 >= totalOut*5 &&
			inCount*10 >= totalIn*5 {
			traits = append(traits, fmt.Sprintf(
				"和%s互动了%d次，同时也被对方互动%d次",
				user.Nickname, outCount, inCount))
			break
		}
	}

	// ---- 4. 单向暗恋：疯狂找某人，但对方没有回找 ----
	if topOutCount >= 5 {
		inCountFromTop := stat.BeReplied[topOut]
		if inCountFromTop == 0 {
			traits = append(traits, fmt.Sprintf(
				"主动与%s互动%d次，但对方没有回复，已读不回的感觉",
				topOut.Nickname, topOutCount))
		}
	}

	// ---- 5. 人气王 vs 空气人 ----
	uniqueIn := len(stat.BeReplied)
	uniqueOut := len(stat.InteractionCount)

	switch {
	case uniqueIn >= 5 && uniqueOut <= 1:
		traits = append(traits, fmt.Sprintf(
			"被%d个不同的人互动，自己几乎不互动", uniqueIn))
	case uniqueOut >= 5 && uniqueIn <= 1:
		traits = append(traits, fmt.Sprintf(
			"主动互动了%d个不同的人，没有被别人互动，社恐克星", uniqueOut))
	case uniqueIn >= 5 && uniqueOut >= 5:
		traits = append(traits, fmt.Sprintf(
			"和%d人互动，被%d人互动，社交中心节点", uniqueOut, uniqueIn))
	}

	// ---- 6. 互动内容风格分析（只分析互动最多的那对） ----
	if topOutCount >= 3 {
		msgs := stat.InteractionMessage[topOut]
		style := analyzeInteractionStyle(msgs)
		if style != "" {
			traits = append(traits, fmt.Sprintf(
				"和%s说话时%s", topOut.Nickname, style))
		}
	}

	// ---- 7. 被互动但不回应 ----
	ignoredCount := 0
	for user, inMsgs := range stat.BeRepliedMessage {
		outCount := stat.InteractionCount[user]
		if len(inMsgs) >= 3 && outCount == 0 {
			ignoredCount++
		}
	}
	if ignoredCount >= 2 {
		traits = append(traits, fmt.Sprintf(
			"被%d个人主动互动，但一个都没回，已读不回冠军", ignoredCount))
	}

	return traits
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

func findSilentTimes(timeStats []TimeStat) []time.Time {
	var silent []time.Time
	for _, stat := range timeStats {
		if stat.Count == 0 {
			silent = append(silent, stat.Time)
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
		Opening:           "💀 余灰们，昨日的篝火记录如下。",
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
		Opening:           "📋 老师收到了昨日的问题学生行为报告。",
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
		Opening:           "🌟 昨日群聊替身使用报告，以下能力已被记录在册。",
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
