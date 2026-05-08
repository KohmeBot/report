package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fumiama/cron"
	"gorm.io/gorm"
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
		text, err := p.BuildReport(ctx.Event.GroupID, time.Now())
		if err != nil {
			p.env.Error(ctx, err)
			return
		}
		ctx.Send(text)
	})
}

func (p *PluginReport) BuildReport(group int64, t time.Time) (string, error) {
	date := t.Format("2006-01-02")

	// 聚合数据
	aggregator := daily.NewAggregator(p.db)
	report, err := aggregator.Aggregate(group, date)
	if err != nil {
		return "", fmt.Errorf("聚合失败: %w", err)
	}
	if report == nil {
		return "", nil
	}

	// 构造prompt，调用AI
	data := daily.BuildPrompt(report)
	req := fmt.Sprintf(daily.UserPrompt, data)
	largeModel, err := p.invoker.NewModel(daily.System, true, false)
	if err != nil {
		return "", err
	}
	res, err := p.invoker.DoRequestWithModel(req, largeModel)
	if err != nil {
		return "", fmt.Errorf("AI调用失败: %w", err)
	}
	logrus.Infof("req:%s\nresp:%s\n", req, res)

	// 持久化聚合数据和AI结果
	dataJSON, _ := json.Marshal(report)
	if err := p.saveReport(group, date, string(dataJSON), res); err != nil {
		// 存储失败不影响返回，只记录日志
		logrus.Warnf("持久化日报失败 group=%d date=%s err=%v", group, date, err)
	}

	return res, nil
}

func (p *PluginReport) saveReport(group int64, date, data, report string) error {
	var stat daily.GroupDailyStat
	err := p.db.Where("group_id = ? AND date = ?", group, date).First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p.db.Create(&daily.GroupDailyStat{
			GroupID: group,
			Date:    date,
			Data:    data,
			Report:  report,
		}).Error
	}
	return p.db.Model(&stat).Updates(map[string]any{
		"data":   data,
		"report": report,
	}).Error
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
