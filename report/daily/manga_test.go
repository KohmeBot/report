package daily

import (
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

	prompt, err := buildMangaBriefPrompt(topics, users)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"topics数量必须恰好为2", "早餐之争", "线上故障", "小甲", "小乙", "最多5人", "具体场景", "禁止空白背景", "composition"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q", want)
		}
	}
	if strings.Index(prompt, `"id":1`) > strings.Index(prompt, `"id":2`) {
		t.Fatal("characters are not emitted in stable id order")
	}
}

func TestNormalizeMangaBriefAndBuildImagePrompt(t *testing.T) {
	topics := []TopicResult{
		{Topic: "早餐之争", Contributors: []int64{2, 1}, Detail: "很长的原始详情不应进入生图提示词。"},
		{Topic: "线上故障", Contributors: []int64{1}, Detail: "另一段很长的原始详情。"},
	}
	users := map[int64]UserImage{
		1: {Id: 1, NickName: "小甲"},
		2: {Id: 2, NickName: "小乙"},
	}
	brief := mangaBrief{
		Title:       "豆花引发的线上危机",
		Story:       "一场早餐争论意外引发服务故障，众人在厨房和机房之间狂奔。",
		Composition: "横向画布，开场用豆花碗破框飞出，中央斜切追逐格，结尾用机房大特写；昵称写在人物首次冲入画面处。",
		Characters: []mangaBriefCharacter{
			{ID: 2, Appearance: "银色短发，圆眼镜，蓝色卫衣，星形发夹"},
			{ID: 1, Appearance: "黑色卷发，方脸，橙色夹克，红围巾"},
		},
		Topics: []mangaBriefTopic{
			{Index: 1, ParticipantIDs: []int64{2, 1}, Direction: "早餐店里两人争抢豆花碗，糖罐和辣椒油在空中飞散；低机位特写，喊‘甜的！’‘咸的！’。"},
			// 小乙不是原话题参与者，但允许作为串联故事的跨topic角色出现。
			{Index: 2, ParticipantIDs: []int64{2}, Direction: "机房警灯大作，小乙抱着键盘滑过地面拍下恢复按钮；屏幕爆出‘恢复！’。"},
		},
	}

	if err := normalizeMangaBrief(&brief, topics, users, false); err != nil {
		t.Fatal(err)
	}
	prompt, err := buildMangaPrompt(brief, "水彩报纸漫画")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"水彩报纸漫画", "豆花引发的线上危机", "横向画布", "不等于固定2格", "早餐店", "机房警灯", "禁止把几个人横排", "小甲", "小乙"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("image prompt does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"很长的原始详情", "MBTI", "groupTitle", "participant_ids"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("image prompt unexpectedly contains %q", unwanted)
		}
	}
}

func TestNormalizeMangaBriefRejectsTooManyCharacters(t *testing.T) {
	users := make(map[int64]UserImage)
	brief := mangaBrief{Title: "标题", Story: "故事", Composition: "构图", Topics: []mangaBriefTopic{{Index: 1, ParticipantIDs: []int64{}, Direction: "场景指令"}}}
	for id := int64(1); id <= 6; id++ {
		users[id] = UserImage{Id: id, NickName: "用户"}
		brief.Characters = append(brief.Characters, mangaBriefCharacter{ID: id, Appearance: "不同外观"})
	}
	if err := normalizeMangaBrief(&brief, []TopicResult{{Topic: "话题"}}, users, false); err == nil {
		t.Fatal("expected too many characters to be rejected")
	}
}

func TestRequestMangaBriefRetriesTooManyCharactersThenAccepts(t *testing.T) {
	users := make(map[int64]UserImage)
	for id := int64(1); id <= 6; id++ {
		users[id] = UserImage{Id: id, NickName: "用户"}
	}
	topics := []TopicResult{{Topic: "话题", Contributors: []int64{1}}}
	calls := 0
	brief, err := requestMangaBrief("原始请求", topics, users, func(request string, output any) error {
		calls++
		if calls > 1 && !strings.Contains(request, "超过最多5人") {
			t.Fatalf("retry request does not contain correction: %q", request)
		}
		characters := make([]mangaBriefCharacter, 0, 6)
		for id := int64(1); id <= 6; id++ {
			characters = append(characters, mangaBriefCharacter{ID: id, Appearance: "有辨识度的外观"})
		}
		*output.(*mangaBrief) = mangaBrief{
			Title:       "标题",
			Story:       "故事主线",
			Composition: "自由构图",
			Characters:  characters,
			Topics: []mangaBriefTopic{{
				Index: 1, ParticipantIDs: []int64{1}, Direction: "角色在具体场景中飞奔。",
			}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("request count = %d, want 4 (initial request plus 3 rewrites)", calls)
	}
	if len(brief.Characters) != 6 {
		t.Fatalf("fallback characters = %d, want 6", len(brief.Characters))
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
