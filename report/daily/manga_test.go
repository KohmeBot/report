package daily

import (
	"strings"
	"testing"
)

func TestBuildMangaPromptContainsAllTopicsAndCharacters(t *testing.T) {
	topics := []TopicResult{
		{Topic: "早餐之争", Contributors: []int64{2, 1}, Detail: "两人讨论甜咸豆花。"},
		{Topic: "线上故障", Contributors: []int64{1}, Detail: "服务恢复。"},
	}
	users := map[int]UserImage{
		2: {Id: 2, NickName: "小乙", Sex: "female", Title: "评论家"},
		1: {Id: 1, NickName: "小甲", Sex: "male", Mbti: "INTJ"},
	}

	prompt, err := buildMangaPrompt(topics, users)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"恰好 2 个场景分镜", "早餐之争", "线上故障", "小甲", "小乙"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q", want)
		}
	}
	if strings.Index(prompt, `"id": 1`) > strings.Index(prompt, `"id": 2`) {
		t.Fatal("characters are not emitted in stable id order")
	}
}

func TestNormalizeImageResult(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   string
	}{
		{name: "url", result: "https://example.com/manga.png", want: "https://example.com/manga.png"},
		{name: "markdown", result: "已生成：![漫画](https://example.com/manga.png)", want: "https://example.com/manga.png"},
		{name: "data url", result: "data:image/png;base64,aGVsbG8=", want: "base64://aGVsbG8="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeImageResult(tt.result)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeImageResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeImageResultRejectsText(t *testing.T) {
	if _, err := normalizeImageResult("图片生成失败"); err == nil {
		t.Fatal("expected an error for a non-image response")
	}
}
