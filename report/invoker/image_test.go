package invoker

import (
	"strings"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
)

func TestExtractImageContent(t *testing.T) {
	tests := []struct {
		name    string
		content any
		want    string
	}{
		{
			name: "typed image part",
			content: []model.ContentPart{
				{Type: "text", Text: "图片已生成"},
				{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://example.com/typed.png"}},
			},
			want: "https://example.com/typed.png",
		},
		{
			name:    "legacy image part",
			content: []any{map[string]any{"image": "https://example.com/legacy.png"}},
			want:    "https://example.com/legacy.png",
		},
		{
			name: "legacy image url part",
			content: []any{
				map[string]any{"image_url": map[string]any{"url": "https://example.com/nested.png"}},
			},
			want: "https://example.com/nested.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractImageContent(tt.content)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("extractImageContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractImageContentReturnsErrorForUnexpectedShape(t *testing.T) {
	_, err := extractImageContent([]model.ContentPart{{Type: "image_url"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "[]model.ContentPart") {
		t.Fatalf("error should describe the response type: %v", err)
	}
}
