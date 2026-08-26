package invoker

import "github.com/kohmebot/chatai/chatai/chataisdk"

type TextInvoker struct {
	invoker  *chataisdk.ChatAIInvoker
	provider string
	model    string
	system   string
	online   bool
	thinking bool
}

func NewTextInvoker(invoker *chataisdk.ChatAIInvoker, provider, model, system string, online bool, thinking bool) *TextInvoker {
	return &TextInvoker{
		invoker:  invoker,
		provider: provider,
		model:    model,
		system:   system,
		online:   online,
		thinking: thinking,
	}
}

func (i *TextInvoker) DoRequest(req string) (string, error) {
	largeModel, err := i.invoker.NewProviderModel(i.provider, i.model, i.system, i.online, i.thinking, false)

	if err != nil {
		return "", err
	}
	return i.invoker.DoRequestWithModel(req, largeModel)
}
