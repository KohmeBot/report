package report

import (
	"fmt"
	"github.com/fumiama/cron"

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
		}

		if !slices.Contains([]string{"text", "image", "at", "poke"}, msgType) {
			return
		}

		msg := daily.GroupMessage{
			GroupID:   ctx.Event.GroupID,
			UserID:    ctx.Event.UserID,
			Nickname:  ctx.CardOrNickName(ctx.Event.UserID),
			Content:   ctx.Event.Message.ExtractPlainText(),
			MsgType:   msgType,
			Hour:      t.Hour(),
			MsgID:     ctx.Event.MessageID.(int64),
			CreatedAt: t,
		}

		p.r.Write(msg)
	})
}

func (p *PluginReport) OnBuild(engine plugin.Engine) {
	engine.OnCommand("buildreport", p.env.SuperUser().Rule()).Handle(func(ctx *zero.Ctx) {
		text, err := p.BuildReport(ctx.Event.GroupID, time.Now())
		if err != nil {
			p.env.Error(ctx, err)
			return
		}
		ctx.Send(text)
	})
}

func (p *PluginReport) BuildReport(group int64, t time.Time) (string, error) {
	aggregator := daily.NewAggregator(p.db)
	report, err := aggregator.Aggregate(group, t.Format("2006-01-02"))
	if err != nil {
		return "", err
	}
	data := daily.BuildPrompt(report)
	req := fmt.Sprintf(daily.UserPrompt, data)
	res, err := p.invoker.DoRequestWithModel(req, p.largeModel)
	if err != nil {
		return "", err
	}
	logrus.Infof("req:%s\nresp:%s\n", req, res)
	return res, nil
}

func (p *PluginReport) startSendTicker() {
	c := cron.New()
	var id cron.EntryID
	id, err := c.AddFunc("0 23 * * *", func() {
		now := time.Now()
		p.env.UseBot(func(ctx *zero.Ctx) {
			for group := range p.env.Groups().RangeGroup() {
				text, err := p.BuildReport(group, now)
				if err != nil {
					p.env.Error(ctx, err)
					return
				}
				ctx.Send(text)
				time.Sleep(5 * time.Second)

			}
		})

		logrus.Infof("Next 将在 %s 发送Rank", c.Entry(id).Next)
	})
	if err != nil {
		logrus.Errorf("开启定时发送失败 %s", err)
		return
	}

	c.Start()
	time.Sleep(300 * time.Millisecond)
	logrus.Infof("将在 %s 发送Rank", c.Entry(id).Next)
}
