package invoker

import (
	"errors"
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/kohmebot/chatai/chatai/model"
)

type ImageInvoker struct {
	invoker  *chataisdk.ChatAIInvoker
	provider string
	model    string
	system   string
	online   bool
	thinking bool
}

func NewImageInvoker(invoker *chataisdk.ChatAIInvoker, provider, model, system string, online bool, thinking bool) *ImageInvoker {
	return &ImageInvoker{
		invoker:  invoker,
		provider: provider,
		model:    model,
		system:   system,
		online:   online,
		thinking: thinking,
	}
}

func (i *ImageInvoker) DoRequest(req string) (string, error) {
	largeModel, err := i.invoker.NewProviderModel(i.provider, i.model, i.system, i.online, i.thinking, false)

	if err != nil {
		return "", err
	}
	response := new(model.Response)
	if err := largeModel.Request(&model.Request{Content: []any{map[string]any{"text": req}}}, response); err != nil {
		return "", err
	}
	if response.ErrorMsg != "" {
		return "", errors.New(response.ErrorMsg)
	}

	content := response.Content.([]any)
	data := content[0].(map[string]any)

	return data["image"].(string), nil
}
