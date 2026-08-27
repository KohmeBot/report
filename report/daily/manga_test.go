package daily

import (
	"strconv"
	"strings"
	"testing"
)

func TestBuildMangaBriefPromptContainsAllSourceData(t *testing.T) {
	topics := []TopicResult{
		{Topic: "早餐之争", Contributors: []int64{2, 1}, Detail: "两人讨论甜咸豆花。"},
		{Topic: "线上故障", Contributors: []int64{1}, Detail: "服务恢复。"},
	}
	users := map[int64]UserImage{
		2: {Id: 2, NickName: "小乙", Sex: "female", Title: "评论家"},
		1: {Id: 1, NickName: "小甲", Sex: "male", Mbti: "INTJ"},
	}

	prompt, err := buildMangaBriefPrompt(topics, users, 600)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"全部2个话题", "早餐之争", "线上故障", "人物ID:1（小甲）", "人物ID:2（小乙）", "没有硬性数量上限", "人物ID硬约束", "不得超过600个字符", "不要输出JSON", "具体场景", "禁止空白背景"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q", want)
		}
	}
	if strings.Index(prompt, `"id":1`) > strings.Index(prompt, `"id":2`) {
		t.Fatal("characters are not emitted in stable id order")
	}
}

func TestNormalizeMangaBriefAndBuildImagePrompt(t *testing.T) {
	topics := []TopicResult{{Topic: "早餐之争"}, {Topic: "线上故障"}}
	users := map[int64]UserImage{
		1: {Id: 1, NickName: "小甲"},
		2: {Id: 2, NickName: "小乙"},
		3: {Id: 3, NickName: "未登场者"},
	}
	brief := "总标题《豆花引发的线上危机》。人物ID:1（小甲）和人物ID:2（小乙）从早餐店一路追到机房。"
	got := normalizeMangaBrief("  "+brief+"  ", topics, users, 600)
	if got != brief {
		t.Fatalf("normalized brief = %q, want %q", got, brief)
	}
	prompt, err := buildMangaPrompt(got, "水彩报纸漫画", len(topics))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"水彩报纸漫画", "导演简报：" + brief, "豆花引发的线上危机", "共2个顺序话题", "早餐店", "避免空背景站排", "人物ID:1（小甲）", "人物ID:2（小乙）"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("image prompt does not contain %q", want)
		}
	}
	if strings.Contains(prompt, "未登场者") {
		t.Fatal("image prompt should only include characters referenced by the brief")
	}
	if strings.Contains(prompt, "%!(EXTRA") {
		t.Fatalf("image prompt contains a formatting artifact: %q", prompt)
	}
	if strings.Contains(prompt, "导演简报：无（不得画人物）") {
		t.Fatalf("image prompt used the character whitelist as the brief: %q", prompt)
	}
}

func TestNormalizeMangaBriefOnlyWarnsAboutUnknownCharacter(t *testing.T) {
	brief := "人物ID:999（虚构人物）冲进画面。"
	got := normalizeMangaBrief(brief, []TopicResult{{Topic: "话题"}}, map[int64]UserImage{1: {Id: 1, NickName: "用户"}}, 600)
	if got != brief {
		t.Fatalf("brief should continue unchanged, got %q", got)
	}
}

func TestNormalizeMangaBriefCapsLengthForShortImageContext(t *testing.T) {
	const maxLength = 123
	brief := "人物ID:1（用户）。" + strings.Repeat("画", maxLength)
	got := normalizeMangaBrief(brief, []TopicResult{{Topic: "话题"}}, map[int64]UserImage{1: {Id: 1, NickName: "用户"}}, maxLength)
	if length := len([]rune(got)); length != maxLength {
		t.Fatalf("brief length = %d, want %d", length, maxLength)
	}
}

func TestRequestMangaBriefDoesNotRetryOrLimitCharacters(t *testing.T) {
	users := make(map[int64]UserImage)
	for id := int64(1); id <= 6; id++ {
		users[id] = UserImage{Id: id, NickName: "用户"}
	}
	topics := []TopicResult{{Topic: "话题", Contributors: []int64{1}}}
	calls := 0
	brief, err := requestMangaBrief("原始请求", topics, users, defaultMangaPromptMaxLength, func(request string) (string, error) {
		calls++
		if request != "原始请求" {
			t.Fatalf("request = %q", request)
		}
		var characters []string
		for id := int64(1); id <= 6; id++ {
			characters = append(characters, "人物ID:"+strconv.FormatInt(id, 10)+"（用户）")
		}
		return strings.Join(characters, "、") + "一起在具体场景中飞奔。", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("request count = %d, want 1", calls)
	}
	if !strings.Contains(brief, "人物ID:6") {
		t.Fatalf("sixth character was unexpectedly removed: %q", brief)
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
