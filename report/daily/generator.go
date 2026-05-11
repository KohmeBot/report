package daily

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"maps"
	"math/rand"
	"slices"
	"strings"
	"time"
)

type Generator struct {
	db      *gorm.DB
	invoker *chataisdk.ChatAIInvoker
}

func NewGenerator(db *gorm.DB, invoker *chataisdk.ChatAIInvoker) *Generator {
	return &Generator{
		db:      db,
		invoker: invoker,
	}
}

func (g *Generator) GenerateReport(group int64, t time.Time, theme *DailyTheme) (string, error) {
	date := t.Format("2006-01-02")

	// 生成日报
	aggregator := NewAggregator(g.db)
	report, err := aggregator.Aggregate(group, date)
	if err != nil {
		return "", fmt.Errorf("聚合失败: %w", err)
	}
	if report == nil {
		logrus.Infof("%d 昨日暂无数据", group)
		return "", nil
	}

	data := g.buildPrompt(report)
	req := fmt.Sprintf(reportPrompt,
		theme.Theme,
		theme.Role,
		theme.Style,
		data,
		theme.Opening,
		// 核心人物
		theme.MvpHeader,
		theme.UserFormat,
		// 关键时刻
		theme.MomentHeader,
		theme.MomentFormat,
		// 社交图谱
		theme.InteractionHeader,
		theme.InteractionFormat,
		// 冷知识
		theme.TriviaHeader,
		theme.TriviaFormat,
		// 群体诊断
		theme.DiagnosisHeader,
		// 失踪人口
		theme.GhostHeader,
		theme.GhostFormat,
	)

	largeModel, err := g.invoker.NewModel(systemPrompt, true, false, false)
	if err != nil {
		return "", err
	}

	res, err := g.invoker.DoRequestWithModel(req, largeModel)
	if err != nil {
		return "", fmt.Errorf("AI调用失败: %w", err)
	}

	dataJSON, _ := json.Marshal(report)
	themeJSON, _ := json.Marshal(theme)
	if err := g.saveReport(group, date, string(dataJSON), res, string(themeJSON)); err != nil {
		logrus.Warnf("持久化失败: %v", err)
	}

	return res, nil
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
	sb.WriteString(fmt.Sprintf("=== %s 群聊日报数据 ===\n\n", r.Date))
	sb.WriteString(fmt.Sprintf("【基本数据】\n今日发言人数：%d人\n今日总消息数：%d条\n\n",
		r.ActiveUsers, r.TotalMsg))

	// 活跃时段
	sb.WriteString("【活跃时段Top5】\n")
	for i, h := range r.HourStats {
		sb.WriteString(fmt.Sprintf("  %d. %02d点 —— %d条\n", i+1, h.Hour, h.Count))
	}

	// 找出最冷清时段（0条发言的小时）
	silentHours := findSilentHours(r.HourStats)
	if len(silentHours) > 0 {
		sb.WriteString(fmt.Sprintf("群沉默时段：%s（大家都不在）\n", formatHours(silentHours)))
	}
	sb.WriteString("\n")

	// 群友排行（只取前10，太多token爆炸）
	sb.WriteString("【群友今日表现】\n")
	limit := len(r.UserStats)
	if limit > 10 {
		limit = 10
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
		imgRate := stat.ImageCount * 100 / stat.MsgCount
		switch {
		case imgRate >= 60:
			traits = append(traits, fmt.Sprintf("发了%d张图占发言60%%+，靠图说话", stat.ImageCount))
		case stat.ImageCount >= 5:
			traits = append(traits, fmt.Sprintf("发了%d张图", stat.ImageCount))
		}
	}

	// 戳一戳
	switch {
	case stat.PokeCount >= 10:
		traits = append(traits, fmt.Sprintf("戳了别人%d次，戳戳狂魔", stat.PokeCount))
	case stat.PokeCount >= 3:
		traits = append(traits, fmt.Sprintf("戳了别人%d次", stat.PokeCount))
	case stat.PokeCount == 1:
		traits = append(traits, "戳了别人1次，不知道什么心态")
	}

	// at别人
	switch {
	case stat.AtCount >= 10:
		traits = append(traits, fmt.Sprintf("@了别人%d次，群里的点名机器", stat.AtCount))
	case stat.AtCount >= 3:
		traits = append(traits, fmt.Sprintf("@了别人%d次", stat.AtCount))
	}

	// 回复行为
	switch {
	case stat.ReplyCount >= 10:
		traits = append(traits, fmt.Sprintf("引用回复了%d次，积极参与型或连环杠精", stat.ReplyCount))
	case stat.ReplyCount >= 3:
		traits = append(traits, fmt.Sprintf("引用回复了%d次", stat.ReplyCount))
	}

	// 被回复
	switch {
	case stat.BeReplied >= 5:
		traits = append(traits, fmt.Sprintf("被别人引用回复或@%d次，发言有人接", stat.BeReplied))
	case stat.BeReplied == 0 && stat.MsgCount >= 30:
		traits = append(traits, "发了这么多条没人引用回复")
	}

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
		traits = append(traits, fmt.Sprintf("连发爆发%d次，间歇性话痨确诊", stat.BurstCount))
	case stat.BurstCount >= 2:
		traits = append(traits, fmt.Sprintf("有%d次60秒内连发3条以上", stat.BurstCount))
	}

	// 情绪
	if stat.ExclamCount >= 10 {
		traits = append(traits, fmt.Sprintf("用了%d个感叹号，今天情绪高涨", stat.ExclamCount))
	}
	if stat.QuestionCount >= 8 {
		traits = append(traits, fmt.Sprintf("问了%d个问号，今天十万个为什么", stat.QuestionCount))
	}
	if stat.EllipsisCount >= 5 {
		traits = append(traits, fmt.Sprintf("用了%d个省略号，意味深长还是说不完整", stat.EllipsisCount))
	}

	// 词汇量 vs 发言量的矛盾
	if stat.MsgCount >= 10 && stat.VocabSize > 0 {
		switch {
		case stat.VocabSize < 20 && stat.MsgCount >= 15:
			traits = append(traits, fmt.Sprintf(
				"发了%d条但只用了%d种字，翻来覆去就那几个词", stat.MsgCount, stat.VocabSize))
		case stat.VocabSize >= 80:
			traits = append(traits, fmt.Sprintf("用词丰富，今天输出了%d种不同的字", stat.VocabSize))
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
			"今天把「%s」说了%d次，人工复读机", preview, stat.RepeatCount))
	}

	// 时间特征
	activeHours := stat.LastHour - stat.FirstHour
	switch {
	case stat.NightOwl && stat.FirstHour >= 22:
		traits = append(traits, fmt.Sprintf(
			"%02d点开始发言%02d点还没睡，全程夜班", stat.FirstHour, stat.LastHour))
	case stat.NightOwl:
		traits = append(traits, fmt.Sprintf(
			"凌晨还在发言，%02d点才消失", stat.LastHour))
	case stat.FirstHour <= 7 && stat.LastHour <= 12:
		traits = append(traits, fmt.Sprintf(
			"%02d点就开始发言，早鸟型，%02d点沉默", stat.FirstHour, stat.LastHour))
	case activeHours >= 14:
		traits = append(traits, fmt.Sprintf(
			"从%02d点聊到%02d点，全天候在线", stat.FirstHour, stat.LastHour))
	case activeHours <= 1 && stat.MsgCount >= 10:
		traits = append(traits, fmt.Sprintf(
			"1小时内集中发了%d条，打完就跑", stat.MsgCount))
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

func findSilentHours(activeHours []HourStat) []int {
	active := make(map[int]bool)
	for _, h := range activeHours {
		active[h.Hour] = true
	}
	silent := []int{}
	for h := 0; h < 24; h++ {
		if !active[h] {
			silent = append(silent, h)
		}
	}
	return silent
}

func formatHours(hours []int) string {
	parts := make([]string, len(hours))
	for i, h := range hours {
		parts[i] = fmt.Sprintf("%02d点", h)
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
	},
}
