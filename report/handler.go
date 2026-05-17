package report

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fumiama/cron"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"

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
			if msgType != daily.MsgTypeText {
				// 找到第一个非text的消息
				break
			}
		}

		if !daily.HasMsgType(msgType) {
			return
		}

		msg := daily.GroupMessage{
			GroupID:      ctx.Event.GroupID,
			UserID:       ctx.Event.UserID,
			TargetUserID: getTargetID(ctx),
			Nickname:     ctx.CardOrNickName(ctx.Event.UserID),
			Content:      ctx.Event.Message.ExtractPlainText(),
			MsgType:      msgType,
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

		theme := p.GetTheme(Yesterday())

		time.Sleep(2 * time.Second)

		report, err := p.GetReport(group, Yesterday(), theme)
		if err != nil {
			p.env.Error(ctx, err)
			return
		}

		switch {
		case p.conf.OnlyText:
			ctx.Send(report.Text)
		case len(report.Image) > 0:
			ctx.Send(message.ImageBytes(report.Image))
		default:
			ctx.Send(report.Text)
		}

	})
}

func (p *PluginReport) OnBuildPrompt(engine plugin.Engine) {
	engine.OnCommand("buildprompt", p.env.SuperUser().Rule()).Handle(func(ctx *zero.Ctx) {
		var cmd extension.CommandModel
		err := ctx.Parse(&cmd)
		if err != nil {
			logrus.Error(err)
		}

		group, _ := strconv.ParseInt(cmd.Args, 10, 64)
		if group == 0 {
			group = ctx.Event.GroupID
		}

		prompt, err := p.GetPrompt(group, Yesterday())
		if err != nil {
			p.env.Error(ctx, err)
			return
		}

		ctx.Send(prompt)

	})
}

func (p *PluginReport) OnSpecifyTheme(engine plugin.Engine) {
	engine.OnCommand("specifytheme", p.env.SuperUser().Rule()).Handle(func(ctx *zero.Ctx) {
		var cmd extension.CommandModel
		err := ctx.Parse(&cmd)
		if err != nil {
			logrus.Error(err)
			return
		}

		cmd.Args = strings.TrimSpace(cmd.Args)
		if cmd.Args == "" {
			p.env.Error(ctx, fmt.Errorf("theme 为空"))
			return
		}

		today := time.Now().Format("2006-01-02")

		var specify daily.SpecifyTheme
		err = p.db.Where("date = ?", today).First(&specify).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			p.env.Error(ctx, err)
			return
		}

		specify.ThemeString = cmd.Args
		specify.Date = today
		err = p.db.Save(&specify).Error
		if err != nil {
			p.env.Error(ctx, err)
			return
		}

		ctx.Send(fmt.Sprintf("设置主题成功：%s，明日将以该主题生成", cmd.Args))
	})
}

func (p *PluginReport) GetTheme(t time.Time) (theme *daily.DailyTheme) {
	defer func() {
		logrus.Infof("今日主题: %+v", theme)
	}()
	var err error
	g := daily.NewGenerator(p.env, p.db, p.invoker, p.conf.ChromeAddr())

	if !p.conf.RegenTheme {
		theme, err = g.GetTodayTheme(t)
		// 已有则复用
		if err == nil {
			return theme
		}
	}

	var excludeDate []time.Time
	for i := 1; i <= 7; i++ {
		// 最近七天
		// 以昨天为基准算，这里要-1
		offset := -i - 1
		excludeDate = append(excludeDate, Day(offset))
	}
	used, err := g.GetUsedTheme(excludeDate...)
	if err != nil {
		logrus.Errorf("获取已使用的主题失败 %v", err)
	}
	specify, err := g.GetSpecifyTheme(t)
	if err != nil {
		logrus.Errorf("获取指定的主题失败 %v", err)
	}

	theme, err = g.GenerateTheme(t, specify.ThemeString, used...)
	if err != nil {
		logrus.Errorf("生成主题失败 %v", err)
		theme = daily.FallbackTheme()
	}

	return theme
}

func (p *PluginReport) GetReport(group int64, t time.Time, theme *daily.DailyTheme) (daily.Report, error) {

	g := daily.NewGenerator(p.env, p.db, p.invoker, p.conf.ChromeAddr())

	report, err := g.GenerateReport(group, t, theme)

	return report, err
}

func (p *PluginReport) GetPrompt(group int64, t time.Time) (string, error) {

	g := daily.NewGenerator(p.env, p.db, p.invoker, p.conf.ChromeAddr())

	prompt, _, err := g.BuildPrompt(group, t)

	return prompt, err
}

func (p *PluginReport) startSendTicker() {
	c := cron.New()
	var id cron.EntryID
	id, err := c.AddFunc("0 9 * * *", func() {
		yesterday := Yesterday()
		theme := p.GetTheme(yesterday)
		iter := p.env.Groups().RangeGroup()
		if p.conf.SendGroups != nil {
			iter = slices.Values(p.conf.SendGroups)
		}
		time.Sleep(2 * time.Second)
		p.env.UseBot(func(ctx *zero.Ctx) {
			for group := range iter {
				report, err := p.GetReport(group, yesterday, theme)
				if err != nil {
					p.env.Error(ctx, err)
					return
				}
				switch {
				case p.conf.OnlyText:
					ctx.SendGroupMessage(group, report.Text)
				case len(report.Image) > 0:
					ctx.SendGroupMessage(group, message.ImageBytes(report.Image))
				default:
					ctx.SendGroupMessage(group, report.Text)
				}

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

func Day(offset ...int) time.Time {
	var os int
	if len(offset) > 0 {
		os = offset[0]
	}

	now := time.Now()
	yesterday := time.Date(
		now.Year(),
		now.Month(),
		now.Day()+os,
		0, 0, 0, 0,
		now.Location(),
	)
	return yesterday
}

func Yesterday() time.Time {
	return Day(-1)
}
