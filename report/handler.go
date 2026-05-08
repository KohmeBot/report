package report

import (
	"fmt"
	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/report/report/daily"
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
			Nickname:  ctx.Event.Sender.NickName,
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
	return res, nil
}
