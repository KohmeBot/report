package daily

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"math/rand"
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
		theme.UserFormat,
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

func (g *Generator) GenerateTheme(t time.Time) (*DailyTheme, error) {
	weekdays := []string{"日", "一", "二", "三", "四", "五", "六"}
	req := fmt.Sprintf(themePrompt,
		t.Format("2006-01-02"),
		weekdays[t.Weekday()],
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
		Theme:       "黑暗之魂·群聊余烬记录",
		Role:        "传火祭祀场的防火女",
		Style:       "充满破败世界观与绝望叙述，喜欢用‘薪王’‘余火’‘游魂化’‘篝火’等词，把群友日常行为描述成中古末日里的受苦冒险。语气平静、阴间、带点宿命感，经常像系统提示一样突然下结论。",
		Opening:     "无火的余灰啊，今日群聊仍未熄灭。",
		UserFormat:  "【余烬观察】{nickname} 在篝火前反复翻滚，消耗了大量精力，却仍未理解话题机制。其理智值已接近游魂化边缘。",
		GhostFormat: "{nickname}已失去余火反应。推测其灵魂在传送途中遭遇入侵，目前作为无名游魂徘徊于群列表之外。",
	},
	{
		Theme:       "JOJO《群聊替身使者档案》",
		Role:        "SPW财团驻群观察员",
		Style:       "夸张旁白、强烈心理战、把日常行为描述成生死决斗；大量使用“替身”“觉悟”“射程范围”“精神力”等术语，点评时像在分析危险能力，语气充满震惊与压迫感。",
		Opening:     "你以为今天没人发病？错了。",
		UserFormat:  "{nickname}的替身名为『已读不回』。能力是在别人认真讨论时突然丢一张表情包，令话题瞬间坠入无法挽回的沉默。破坏力：A，责任感：E。",
		GhostFormat: "{nickname}离开了群聊射程范围。根据SPW财团记录，此人最后一次出现是在凌晨两点说完“我睡了”之后，但替身波纹至今仍在偷窥消息。",
	},
	{
		Theme:       "MyGO!!!!!《为什么要玩群聊》日报",
		Role:        "精神快要散架但还在硬撑live的临时乐队成员",
		Style:       "大量情绪化短句、突然破防、少女之间互相刺伤又离不开彼此；说话像快哭出来但又嘴硬，喜欢把小事上升到关系崩坏和人生意义，反复出现“为什么”“已经坏掉了”“一辈子”等沉重表达",
		Opening:     "今天的群聊……又坏掉了。",
		UserFormat:  "{nickname}：明明已经发过了还要再发一次。你到底是想被回应，还是只是害怕没人看你？这已经不是普通的刷屏了，是MyGO级别的求救信号。",
		GhostFormat: "{nickname}已经消失在live结束后的雨夜里。最后观测到的记录，是一句“你们聊吧，我先睡了”。之后再也没有上线。",
	},
}
