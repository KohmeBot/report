package report

import (
	"fmt"
	"github.com/fumiama/cron"
	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/report/report/daily"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"github.com/wdvxdr1123/ZeroBot/message"
	"strconv"

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
			Url:          getUrl(ctx.Event.Message),
			Nickname:     ctx.CardOrNickName(ctx.Event.UserID),
			Content:      getText(ctx.Event.Message),
			MsgType:      msgType,
			MsgID:        ctx.Event.MessageID.(int64),
			CreatedAt:    t,
		}

		p.r.Write(msg)
	})
}

func getUrl(msgs message.Message) string {
	for _, m := range msgs {
		if m.Type == daily.MsgTypeImg {
			return m.Data["url"]
		}
	}
	return ""
}

func getText(msgs message.Message) string {
	return msgs.ExtractPlainText()
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

		groupName := ctx.GetGroupInfo(group, false).Name

		report, err := p.GetReport(group, groupName, Yesterday())
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
		if report.Manga != "" {
			ctx.Send(message.Image(report.Manga))
		}

	})
}

func (p *PluginReport) GetReport(group int64, groupName string, t time.Time) (daily.Report, error) {

	g := daily.NewGenerator(p.env, p.db, p.invoker,
		p.conf.ProviderName,
		p.conf.ModelName,
		p.conf.Manga.ProviderName,
		p.conf.Manga.ModelName,
		p.conf.ChromeAddr(), p.conf.Thinking, p.conf.Online).
		SetMangaStyle(p.conf.Manga.Style)

	report, err := g.GenerateReport(p.conf.Title, group, groupName, t, p.conf.Manga.EnabledFor(group))

	return report, err
}

func (p *PluginReport) sendReport(ctx *zero.Ctx, t time.Time, group int64) error {
	count, err := daily.NewAggregator(p.db).MessageCount(group, t, 24*time.Hour)
	if err != nil {
		return err
	}
	if count < p.conf.ReportMinMessageCount {
		logrus.Infof("群%d消息数(%d)低于%d，不发送日报", group, count, p.conf.ReportMinMessageCount)
		return nil
	}

	groupName := ctx.GetGroupInfo(group, false).Name
	report, err := p.GetReport(group, groupName, t)
	if err != nil {
		return err
	}
	switch {
	case p.conf.OnlyText:
		ctx.SendGroupMessage(group, report.Text)
	case len(report.Image) > 0:
		ctx.SendGroupMessage(group, message.ImageBytes(report.Image))
	default:
		ctx.SendGroupMessage(group, report.Text)
	}
	if report.Manga != "" {
		ctx.SendGroupMessage(group, message.Image(report.Manga))
	}
	return nil
}

func (p *PluginReport) startSendTicker() {
	var id cron.EntryID
	var cronStr string
	t, err := time.Parse("15:04", p.conf.SendTime)
	if err != nil {
		logrus.Errorf("parse time err %s", err)
		cronStr = "0 9 * * *"
	} else {
		cronStr = fmt.Sprintf("%d %d * * *", t.Minute(), t.Hour())
	}
	c := cron.New()

	id, err = c.AddFunc(cronStr, func() {
		yesterday := Yesterday()
		iter := p.env.Groups().RangeGroup()
		if p.conf.SendGroups != nil {
			iter = slices.Values(p.conf.SendGroups)
		}
		time.Sleep(2 * time.Second)
		var failGroups []int64
		p.env.UseBot(func(ctx *zero.Ctx) {
			for group := range iter {
				err := p.sendReport(ctx, yesterday, group)
				if err != nil {
					p.env.Error(ctx, fmt.Errorf("sendReport %d err:%w", group, err))
					failGroups = append(failGroups, group)
				}
				time.Sleep(3 * time.Second)
			}
		})

		p.env.UseBot(func(ctx *zero.Ctx) {
			for _, group := range failGroups {
				var retry int
			RETRY:
				for {
					if retry >= 3 {
						p.env.Error(ctx, fmt.Errorf("sendReport %d retry fail", group))
						break RETRY
					}
					time.Sleep(3*time.Second + time.Duration(retry*3)*time.Second)
					err := p.sendReport(ctx, yesterday, group)
					if err != nil {
						p.env.Error(ctx, fmt.Errorf("sendReport %d retry err:%w", group, err))
					}

					retry++
				}
			}
		})

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
