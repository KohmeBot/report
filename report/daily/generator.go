package daily

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"net/http"
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

func (g *Generator) botUid() int64 {
	var uid int64
	g.env.UseBot(func(ctx *zero.Ctx) {
		uid = ctx.GetLoginInfo().Get("user_id").Int()
	})
	return uid
}

func (g *Generator) BuildPrompt(group int64, t time.Time) (Prompts, *AggregateData, error) {

	// 生成日报
	aggregator := NewAggregator(g.db, g.invoker, g.thinking)
	report, ump, err := aggregator.Aggregate(group, t, 24*time.Hour)
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

func (g *Generator) GenerateReport(title string, group int64, groupName string, t time.Time) (Report, error) {

	prompts, data, err := g.BuildPrompt(group, t)
	if err != nil {
		return Report{}, fmt.Errorf("生成日报失败: %w", err)
	}
	if data == nil {
		return Report{}, nil
	}

	res, err := g.InvokeAi(prompts)
	if err != nil {
		return Report{}, fmt.Errorf("生成日报失败: %w", err)
	}

	renderData := g.buildRenderData(title, group, groupName, data, &res)

	imgBytes, err := render.RenderToImage(renderData, g.chromeAddr,
		render.WithGeneratedBy(g.botNickName()),
		render.WithGeneratedAt(time.Now().Format("2006-01-02 15:04:05")),
		render.WithUserDataGetter(g.getUserDataFunc(group)),
	)

	return Report{
		Text:  "", // TODO 生成图片失败时，fallback为文本
		Image: imgBytes,
	}, err
}

func (g *Generator) buildRenderData(title string, group int64, groupName string, data *AggregateData, res *AIResult) *render.ReportData {
	report := &render.DailyReport{
		Title:     template.HTML(title),
		GroupName: groupName,
		GroupID:   strconv.FormatInt(group, 10),
		Date:      data.Date.Format("2006年 01月 02日"),
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
		Topics:       make([]*render.TopicItem, 0),
		UserData:     make([]*render.UserItem, 0),
		GoldenData:   make([]*render.GoldenItem, 0),
		GroupQuality: nil,
	}

	for i, topic := range res.Topics {
		reportData.Topics = append(reportData.Topics, &render.TopicItem{
			Index:        i + 1,
			Topic:        topic.Topic,
			Contributors: topic.Contributors,
			Detail:       topic.Detail,
		})
	}
	for _, user := range res.UserResult {
		reportData.UserData = append(reportData.UserData, &render.UserItem{
			User:   user.User,
			Title:  user.Title,
			Mbti:   user.Mbti,
			Reason: user.Reason,
		})
	}
	for _, golden := range res.Goldens {
		// 找到原句
		idx := slices.IndexFunc(data.GroupMessages, func(m GroupMessage) bool {
			return m.MsgID == golden.MsgId
		})
		if idx == -1 {
			continue
		}
		msg := data.GroupMessages[idx]

		reportData.GoldenData = append(reportData.GoldenData, &render.GoldenItem{
			Content: msg.Content,
			Sender:  msg.UserID,
			Reason:  golden.Reason,
			Time:    msg.CreatedAt.Format("15:04"),
		})
	}
	reportData.GroupQuality = &render.GroupQuality{
		Title:      res.Qualities.Title,
		Subtitle:   res.Qualities.Subtitle,
		Dimensions: make([]render.Dimension, 0),
		Summary:    res.Qualities.Summary,
		AIUser:     g.botUid(),
	}
	for _, d := range res.Qualities.Dimensions {
		reportData.GroupQuality.Dimensions = append(reportData.GroupQuality.Dimensions, render.Dimension{
			Name:       d.Name,
			Percentage: d.Percentage,
			Comment:    d.Comment,
		})
	}
	return &reportData
}

func (g *Generator) getUserDataFunc(group int64) func(uid int64) (nickName string, avatar string) {
	cache := map[int64][2]string{}
	var selfId int64
	return func(uid int64) (nickName string, avatar string) {
		g.env.UseBot(func(ctx *zero.Ctx) {
			if selfId == 0 {
				selfId = ctx.GetLoginInfo().Get("user_id").Int()
			}

			data, ok := cache[uid]
			if ok {
				nickName, avatar = data[0], data[1]
				return
			}

			nickName = ctx.GetGroupMemberInfo(group, uid, false).Get("card").String()
			if nickName == "" {
				nickName = ctx.GetStrangerInfo(uid, false).Get("nickname").String()
			}

			if uid == selfId {
				nickName = g.botNickName()
			}

			resp, err := http.Get(fmt.Sprintf("https://q4.qlogo.cn/g?b=qq&nk=%d&s=%d", uid, 640))
			if err != nil {
				return
			}
			defer resp.Body.Close()
			img, _, err := image.Decode(resp.Body)
			if err != nil {
				return
			}

			var buf bytes.Buffer

			err = png.Encode(&buf, img)
			if err != nil {
				return
			}

			avatar = base64.StdEncoding.EncodeToString(buf.Bytes())

			cache[uid] = [2]string{nickName, avatar}
		})
		return
	}

}

func (g *Generator) InvokeAi(prompts Prompts) (res AIResult, err error) {
	iv := invoker.NewJsonInvoker(g.invoker, "", g.online, g.thinking)

	if err = iv.DoRequest(prompts.TopicPrompt, &res.Topics); err != nil {
		return
	}
	if err = iv.DoRequest(prompts.GoldenPrompt, &res.Goldens); err != nil {
		return
	}
	if err = iv.DoRequest(prompts.QualityPrompt, &res.Qualities); err != nil {
		return
	}
	// user放在最后，让上面的可以吃到token cache
	if err = iv.DoRequest(prompts.UserPrompt, &res.UserResult); err != nil {
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

	header := fmt.Sprintf(commonHeader, msgsText)

	prompts.TopicPrompt = header + topicPrompt

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

	prompts.GoldenPrompt = header + goldenPrompt

	prompts.QualityPrompt = header + qualityPrompt

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
