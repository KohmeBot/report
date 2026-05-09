package report

import (
	"github.com/fumiama/cron"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"strconv"

	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/report/report/daily"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"

	"slices"
	"time"
)

func (p *PluginReport) OnHandleMessage(engine plugin.Engine) {
	engine.OnMessage(p.env.Groups().Rule()).Handle(func(ctx *zero.Ctx) {
		t := time.Unix(ctx.Event.Time, 0)
		var msgType string
		for _, segment := range ctx.Event.Message {
			msgType = segment.Type
			if msgType != "text" {
				// 找到第一个非text的消息
				break
			}
		}

		if !slices.Contains([]string{"text", "image", "at", "poke", "reply"}, msgType) {
			return
		}

		msg := daily.GroupMessage{
			GroupID:      ctx.Event.GroupID,
			UserID:       ctx.Event.UserID,
			TargetUserID: getTargetID(ctx),
			Nickname:     ctx.CardOrNickName(ctx.Event.UserID),
			Content:      ctx.Event.Message.ExtractPlainText(),
			MsgType:      msgType,
			Hour:         t.Hour(),
			MsgID:        ctx.Event.MessageID.(int64),
			CreatedAt:    t,
		}

		p.r.Write(msg)
	})
}

func getTargetID(ctx *zero.Ctx) int64 {
	if ctx.Event.TargetID != 0 {
		return ctx.Event.TargetID
	}
	var targetID int64
	// 优先找at
	for _, segment := range ctx.Event.Message {
		if segment.Type == "at" {
			targetID, _ = strconv.ParseInt(segment.Data["qq"], 10, 64)
			break
		}
	}
	if targetID != 0 {
		return targetID
	}

	// 找引用的消息
	for _, segment := range ctx.Event.Message {
		if segment.Type == "reply" {
			msgId, _ := strconv.ParseInt(segment.Data["id"], 10, 64)
			m := ctx.GetMessage(msgId)
			targetID = m.Sender.ID
			break
		}
	}

	return targetID

}

func (p *PluginReport) OnBuild(engine plugin.Engine) {
	engine.OnCommand("buildreport", p.env.SuperUser().Rule()).Handle(func(ctx *zero.Ctx) {
		var cmd extension.CommandModel
		err := ctx.Parse(&cmd)
		if err != nil {
			logrus.Error(err)
		}

		group, _ := strconv.ParseInt(cmd.Args, 10, 64)
		if group == 0 {
			group = ctx.Event.GroupID
		}

		text, err := p.GetReport(group, Yesterday(), p.GetTheme(time.Now()))
		if err != nil {
			p.env.Error(ctx, err)
			return
		}
		ctx.Send(text)
	})
}

func (p *PluginReport) GetTheme(t time.Time) *daily.DailyTheme {
	g := daily.NewGenerator(p.db, p.invoker)
	theme, err := g.GenerateTheme(t)
	if err != nil {
		logrus.Errorf("生成主题失败 %v", err)
		theme = daily.FallbackTheme()
	}
	logrus.Infof("今日主题: %+v", theme)

	return theme
}

func (p *PluginReport) GetReport(group int64, t time.Time, theme *daily.DailyTheme) (string, error) {

	g := daily.NewGenerator(p.db, p.invoker)

	report, err := g.GenerateReport(group, t, theme)

	return report, err
}

func (p *PluginReport) startSendTicker() {
	c := cron.New()
	var id cron.EntryID
	id, err := c.AddFunc("0 8 * * *", func() {
		now := time.Now()
		yesterday := Yesterday()
		theme := p.GetTheme(now)

		p.env.UseBot(func(ctx *zero.Ctx) {
			for group := range p.env.Groups().RangeGroup() {
				text, err := p.GetReport(group, yesterday, theme)
				if err != nil {
					p.env.Error(ctx, err)
					return
				}
				ctx.SendGroupMessage(group, text)
				time.Sleep(3 * time.Second)
			}
		})

		logrus.Infof("Next 将在 %s 发送Report", c.Entry(id).Next)
	})
	if err != nil {
		logrus.Errorf("开启定时发送失败 %s", err)
		return
	}

	c.Start()
	time.Sleep(300 * time.Millisecond)
	logrus.Infof("将在 %s 发送Report", c.Entry(id).Next)
}

func Yesterday() time.Time {
	today := time.Now().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	return yesterday
}
