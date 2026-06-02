package daily

import (
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kohmebot/report/report/daily/render"

	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/report/report/invoker"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gorm.io/gorm"
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
	thinking   bool
	online     bool
}

func NewGenerator(env plugin.Env, db *gorm.DB, invoker *chataisdk.ChatAIInvoker, chromeAddr string, thinking bool, online bool) *Generator {
	return &Generator{
		env:        env,
		db:         db,
		invoker:    invoker,
		chromeAddr: chromeAddr,
		thinking:   thinking,
		online:     online,
	}
}

func (g *Generator) botNickName() string {
	//  NICKNAME
	if len(zero.BotConfig.NickName) > 0 {
		return zero.BotConfig.NickName[0]
	}
	return "bot"
}

func (g *Generator) BuildPrompt(group int64, t time.Time) (Prompts, *AggregateData, error) {
	date := t.Format("2006-01-02")

	// 生成日报
	aggregator := NewAggregator(g.db, g.invoker, g.thinking)
	report, ump, err := aggregator.Aggregate(group, date)
	if err != nil {
		return Prompts{}, nil, fmt.Errorf("聚合失败: %w", err)
	}
	if report == nil {
		logrus.Infof("%d 昨日暂无数据", group)
		return Prompts{}, nil, nil
	}

	data := g.buildPrompt(report, ump)
	return data, report, nil
}

func (g *Generator) makeHourlyDistribution(totalMsg int, timeStats []TimeStat) []render.HourSlot {
	timeStats = slices.Clone(timeStats)
	// 把timeStats按时间升序
	slices.SortFunc(timeStats, func(a, b TimeStat) int {
		return a.Time.Compare(b.Time)
	})

	res := make([]render.HourSlot, 0, len(timeStats))
	for _, t := range timeStats {
		res = append(res, render.HourSlot{
			Count:      t.Count,
			Hour:       t.Time.Hour(),
			Percentage: (float64(totalMsg) / float64(t.Count)) * 100,
		})
	}
	return res
}

func (g *Generator) makeHighlightTime(hotPeriod HotPeriod) string {
	start, end := hotPeriod.Start.Format("15:04"), hotPeriod.End.Format("15:04")
	if start == end {
		return start
	}
	return fmt.Sprintf("%s~%s", start, end)
}

func (g *Generator) fullRenderUsers(group int64, data *render.ReportData) {
	var ctx *zero.Ctx
	g.env.UseBot(func(c *zero.Ctx) {
		ctx = c
	})
	if ctx == nil {
		return
	}
	for _, topic := range data.Topics {
		for _, contributor := range topic.Contributors {
			contributor.Full(ctx, group)
		}
	}
	for _, datum := range data.UserData {
		datum.User.Full(ctx, group)
	}
	for _, datum := range data.GoldenData {
		datum.Sender.Full(ctx, group)
		datum.Contributors = make([]*render.User, 0)

		// 提取 datum.Content 中类似 [123456] 的文本，解析为 user 填入 datum.Contributors 中，并去重
		re := regexp.MustCompile(`\[(\d+)\]`)
		matches := re.FindAllStringSubmatch(datum.Content, -1)

		existing := make(map[int64]bool)
		for _, c := range datum.Contributors {
			existing[c.UserID] = true
		}

		for _, match := range matches {
			uid, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil || existing[uid] {
				continue
			}
			existing[uid] = true
			user := &render.User{UserID: uid}
			user.Full(ctx, group)
			datum.Contributors = append(datum.Contributors, user)
		}
	}

	data.GroupQuality.AIUser = &render.User{
		UserID: ctx.GetLoginInfo().Get("user_id").Int(),
	}
	data.GroupQuality.AIUser.Full(ctx, group)
	data.GroupQuality.AIUser.Nickname = g.botNickName()

}

func (g *Generator) GenerateReport(title string, group int64, groupName string, t time.Time) (Report, error) {

	prompts, data, err := g.BuildPrompt(group, t)
	if err != nil {
		return Report{}, fmt.Errorf("生成日报失败: %w", err)
	}
	if data == nil {
		return Report{}, nil
	}

	topics, users, goldens, quality, err := g.InvokeAi(prompts)
	if err != nil {
		return Report{}, fmt.Errorf("生成日报失败: %w", err)
	}

	report := &render.DailyReport{
		Title:     template.HTML(title), // TODO
		GroupName: groupName,
		GroupID:   strconv.FormatInt(group, 10),
		Date:      t.Format("2006年 01月 02日"),
		Stats: render.Stats{
			TotalMessages:      data.TotalMsg,
			ActiveUsers:        data.ActiveUsers,
			CharCount:          data.TotalCharCount,
			MemeCount:          data.TotalMemeCount,
			HighLightTime:      g.makeHighlightTime(data.HotPeriod),
			HourlyDistribution: g.makeHourlyDistribution(data.TotalMsg, data.TimeStats),
		},
	}

	reportData := render.ReportData{
		Report:       report,
		Topics:       topics,
		UserData:     users,
		GoldenData:   goldens,
		GroupQuality: quality,
	}

	g.fullRenderUsers(group, &reportData)

	imgBytes, err := render.RenderToImage(&reportData, g.chromeAddr, render.WithGeneratedBy(g.botNickName()), render.WithGeneratedAt(time.Now().Format("2006-01-02 15:04:05")))

	return Report{
		Text:  "", // TODO 生成图片失败时，fallback为文本
		Image: imgBytes,
	}, err
}

func (g *Generator) InvokeAi(prompts Prompts) (topics []*render.TopicItem, users []*render.UserItem, goldens []*render.GoldenItem, quality *render.GroupQuality, err error) {
	iv := invoker.NewJsonInvoker(g.invoker, "", g.online, g.thinking)

	if err = iv.DoRequest(prompts.TopicPrompt, &topics); err != nil {
		return
	}
	if err = iv.DoRequest(prompts.UserPrompt, &users); err != nil {
		return
	}
	if err = iv.DoRequest(prompts.GoldenPrompt, &goldens); err != nil {
		return
	}
	if err = iv.DoRequest(prompts.QualityPrompt, &quality); err != nil {
		return
	}

	return

}

// buildPrompt 把DailyReport拼成喂给AI的结构化文本
func (g *Generator) buildPrompt(r *AggregateData, ump map[int64]User) Prompts {
	prompts := Prompts{
		TopicPrompt:   "",
		UserPrompt:    "",
		GoldenPrompt:  "",
		QualityPrompt: "",
	}

	msgs := compressMessages(contentMessage(r.GroupMessages))
	msgsText := formatMessages(msgs, ump)

	prompts.TopicPrompt = fmt.Sprintf(topicPrompt, msgsText)

	users := r.UserStats
	if len(users) > 8 {
		// 最多选8名
		users = users[:8]
	}
	var builder strings.Builder
	for i, stat := range users {
		block := g.buildUserBlock(i+1, stat, ump)
		builder.WriteString(block)
		builder.WriteByte('\n')
	}

	prompts.UserPrompt = fmt.Sprintf(userPrompt, builder.String())

	prompts.GoldenPrompt = fmt.Sprintf(goldenPrompt, msgsText)

	prompts.QualityPrompt = fmt.Sprintf(qualityPrompt, msgsText)

	return prompts

}

func (g *Generator) buildUserBlock(rank int, stat UserStat, ump map[int64]User) string {
	var sb strings.Builder

	// 基础行
	sb.WriteString(fmt.Sprintf("%d. %s｜发言%d条", rank, stat.Nickname, stat.MsgCount))

	// 时间跨度（一行，让AI自己判断有没有梗）
	if !stat.FirstMessage.CreatedAt.IsZero() && !stat.EndMessage.CreatedAt.IsZero() {

		sb.WriteString(fmt.Sprintf("｜第一条发言于%s 最后发言于%s",
			formatTime(stat.FirstMessage.CreatedAt),
			formatTime(stat.EndMessage.CreatedAt)))
		if stat.NightOwl {
			sb.WriteString("｜有凌晨发言")
		}
	}
	sb.WriteString("\n")

	// 首末发言内容
	sb.WriteString(fmt.Sprintf("   第一条消息：%s\n", formatMessage(stat.FirstMessage, ump)))
	sb.WriteString(fmt.Sprintf("   最后一条消息：%s\n", formatMessage(stat.EndMessage, ump)))

	// 消息类型分布（只列出>0的）
	typeDesc := []string{}
	for typ, n := range stat.MsgTypeCount {
		if n > 0 {
			typeDesc = append(typeDesc, fmt.Sprintf("%s%d", MsgTypeString(typ), n))
		}
	}

	if len(typeDesc) > 0 {
		sb.WriteString(fmt.Sprintf("   发言类型和数量：%s\n", strings.Join(typeDesc, ",")))
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

	sb.WriteString("\n")
	return sb.String()
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
