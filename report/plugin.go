package report

import (
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/report/report/daily"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gorm.io/gorm"
)

type PluginReport struct {
	env plugin.Env
	db  *gorm.DB
	r   *BatchRecorder

	invoker *chataisdk.ChatAIInvoker
}

func NewPlugin() plugin.Plugin {
	return new(PluginReport)
}

func (p *PluginReport) OnInit(engine plugin.Engine, env plugin.Env) error {
	var err error
	p.env = env
	p.db, err = env.GetDB()
	if err != nil {
		return err
	}
	err = p.db.AutoMigrate(&daily.GroupMessage{})
	if err != nil {
		return err
	}
	err = p.db.AutoMigrate(&daily.GroupDailyStat{})
	if err != nil {
		return err
	}

	p.invoker, err = chataisdk.NewChatAIInvoker(env)
	if err != nil {
		return err
	}

	p.r = NewBatchRecorder(p.db)

	p.OnHandleMessage(engine)
	p.OnBuild(engine)

	return nil
}

func (p *PluginReport) OnBoot() {
	go p.startSendTicker()
	go p.r.batchWriter()
}

func (p *PluginReport) OnHelp(ctx *zero.Ctx) {

}

func (p *PluginReport) Name() string {
	return "report"
}

func (p *PluginReport) Version() string {
	return "v0.0.1-alpha.6"
}
