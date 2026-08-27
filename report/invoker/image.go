package invoker

import (
	"errors"
	"fmt"
	"strings"

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

	if response.Answer != "" {
		return response.Answer, nil
	}

	return extractImageContent(response.Content)
}

// extractImageContent accepts both the SDK's typed multimodal response and the
// legacy map representation. Providers do not all expose generated images in
// exactly the same shape, so malformed or unexpected responses must become
// normal errors instead of panics in the bot handler.
func extractImageContent(content any) (string, error) {
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
	case []model.ContentPart:
		var text string
		for _, part := range value {
			if part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
				return part.ImageURL.URL, nil
			}
			if text == "" && strings.TrimSpace(part.Text) != "" {
				text = part.Text
			}
		}
		if text != "" {
			return text, nil
		}
	case []any:
		for _, part := range value {
			if result, ok := extractLegacyImagePart(part); ok {
				return result, nil
			}
		}
	case map[string]any:
		if result, ok := extractLegacyImagePart(value); ok {
			return result, nil
		}
	}

	return "", fmt.Errorf("生图模型返回了无法识别的内容格式 %T", content)
}

func extractLegacyImagePart(part any) (string, bool) {
	switch value := part.(type) {
	case string:
		return value, strings.TrimSpace(value) != ""
	case map[string]any:
		for _, key := range []string{"image", "url", "text"} {
			if result, ok := value[key].(string); ok && strings.TrimSpace(result) != "" {
				return result, true
			}
		}
		if imageURL, ok := value["image_url"].(map[string]any); ok {
			if result, ok := imageURL["url"].(string); ok && strings.TrimSpace(result) != "" {
				return result, true
			}
		}
	}
	return "", false
}
