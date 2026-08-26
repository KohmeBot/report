package daily

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kohmebot/report/report/invoker"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)`)
	httpImagePattern     = regexp.MustCompile(`https?://[^\s<>"']+`)
)

const defaultMangaStyle = "萌系日系Q版二维彩色条漫，2.5~3头身，大头小身，深色圆润勾线，平涂加轻赛璐璐阴影，明快柔和"

const mangaBriefSystem = `你是漫画分镜编辑。只返回符合要求的JSON，不要输出Markdown或解释。`

const mangaBriefPrompt = `把下面的原始人物和话题压缩成给生图模型使用的绘图简报。

要求：
1. characters最多5人。优先选择能覆盖更多话题、辨识度更高的人；候选不超过5人时全部保留。
2. 每人的appearance只用一句短句描述可见外观：发型/发色、脸型或眼镜、服装和主色、一个显著配件。人物之间至少有3处明显不同，一眼能区分。
3. 不要描述画风、画质、镜头语言，不要写性格分析；性别unknown时不要擅自确定性别。
4. topics数量必须恰好为%d，index从1连续递增，顺序不变。summary用1~2句概括原话题中值得画出的事件、动作和简短中文对白，总体尽量短，不编造结果。
5. participant_ids只能使用入选characters的id，并且必须是该原话题的参与者；没有合适人物时返回空数组。

输出JSON格式：
{
  "characters": [{"id": 123, "appearance": "一句外观描述"}],
  "topics": [{"index": 1, "participant_ids": [123], "summary": "一两句画面概括"}]
}

原始人物JSON：
%s

原始话题JSON：
%s`

const mangaPrompt = `画一张完整的竖版彩色中文多格漫画。

风格：%s。

严格规则：
- 角色表是唯一人物来源；同一id全图始终保持相同外观，不添加其他人物。
- 顶部画“登场人物”角色卡，每人一张，只写昵称。
- 角色卡之后恰好画%d格；每个topic一格，按index顺序，不合并、不遗漏。
- 每格原样写标题，只画participants中的人物，并用summary表现事件。
- 每格放1~3处简短清晰的中文对白、旁白或拟声词；表情动作鲜明。

角色表：
%s

分镜表：
%s

直接生成图片，不输出说明。`

type UserImage struct {
	Id         int64  `json:"id,omitempty"`
	NickName   string `json:"nickname,omitempty"`
	Mbti       string `json:"mbti,omitempty"`
	Title      string `json:"title,omitempty"`
	Reason     string `json:"reason,omitempty"`
	GroupTitle string `json:"groupTitle,omitempty"`
	Sex        string `json:"sex,omitempty"`
}

type mangaSourceUser struct {
	ID         int64  `json:"id"`
	NickName   string `json:"nickname"`
	Mbti       string `json:"mbti,omitempty"`
	Title      string `json:"title,omitempty"`
	Reason     string `json:"reason,omitempty"`
	GroupTitle string `json:"groupTitle,omitempty"`
	Sex        string `json:"sex,omitempty"`
}

type mangaSourceTopic struct {
	Index          int     `json:"index"`
	Title          string  `json:"title"`
	ContributorIDs []int64 `json:"contributor_ids"`
	Detail         string  `json:"detail"`
}

type mangaBrief struct {
	Characters []mangaBriefCharacter `json:"characters"`
	Topics     []mangaBriefTopic     `json:"topics"`
}

type mangaBriefCharacter struct {
	ID         int64  `json:"id"`
	NickName   string `json:"nickname,omitempty"`
	Appearance string `json:"appearance"`
}

// IsEmpty 只校验文本模型需要返回的字段；nickname 会在服务端用可信原始数据补回。
func (c mangaBriefCharacter) IsEmpty() bool {
	return c.ID == 0 || strings.TrimSpace(c.Appearance) == ""
}

type mangaBriefTopic struct {
	Index          int      `json:"index"`
	Title          string   `json:"title,omitempty"`
	ParticipantIDs []int64  `json:"participant_ids"`
	Participants   []string `json:"participants,omitempty"`
	Summary        string   `json:"summary"`
}

type mangaImageTopic struct {
	Index        int      `json:"index"`
	Title        string   `json:"title"`
	Participants []string `json:"participants"`
	Summary      string   `json:"summary"`
}

// IsEmpty 允许明确的空参与者数组；title 和 participants 由服务端补回。
func (t mangaBriefTopic) IsEmpty() bool {
	return t.Index <= 0 || t.ParticipantIDs == nil || strings.TrimSpace(t.Summary) == ""
}

func (g *Generator) BuildUserImages(group int64, ts []TopicResult, us []UserResult) (map[int64]UserImage, error) {
	ump := make(map[int64]struct{})
	for _, t := range ts {
		for _, contributor := range t.Contributors {
			ump[contributor] = struct{}{}
		}
	}

	res := make(map[int64]UserImage, len(ump))
	var selfId int64
	g.env.UseBot(func(ctx *zero.Ctx) {
		for uid := range ump {
			if selfId == 0 {
				selfId = ctx.GetLoginInfo().Get("user_id").Int()
			}
			memberInfo := ctx.GetGroupMemberInfo(group, uid, false)
			nickName := memberInfo.Get("card").String()
			if nickName == "" {
				nickName = memberInfo.Get("nickname").String()
			}
			if uid == selfId {
				nickName = g.botNickName()
			}
			if nickName == "" {
				logrus.Errorf("用户 %d 的昵称为空", uid)
				continue
			}

			img := UserImage{Id: uid, NickName: nickName, GroupTitle: memberInfo.Get("title").String(), Sex: memberInfo.Get("sex").String()}
			for _, u := range us {
				if u.User == uid {
					img.Mbti, img.Title, img.Reason = u.Mbti, u.Title, u.Reason
					break
				}
			}
			res[uid] = img
		}
	})

	logrus.Infof("用户印象: %v", res)
	return res, nil
}

// BuildManga 先用文本模型把原始资料压缩为绘图简报，再交给小上下文的生图模型。
func (g *Generator) BuildManga(ts []TopicResult, users map[int64]UserImage) (string, error) {
	if len(ts) == 0 {
		return "", nil
	}

	briefRequest, err := buildMangaBriefPrompt(ts, users)
	if err != nil {
		return "", err
	}
	var brief mangaBrief
	textInvoker := invoker.NewJsonInvoker(g.invoker, g.provider, g.model, mangaBriefSystem, false, false)
	if err = textInvoker.DoRequest(briefRequest, &brief); err != nil {
		return "", fmt.Errorf("生成漫画绘图简报失败: %w", err)
	}
	if err = normalizeMangaBrief(&brief, ts, users); err != nil {
		return "", fmt.Errorf("漫画绘图简报无效: %w", err)
	}

	prompt, err := buildMangaPrompt(brief, g.mangaStyle)
	if err != nil {
		return "", err
	}
	result, err := invoker.NewImageInvoker(g.invoker, g.providerManga, g.modelManga, "", false, true).DoRequest(prompt)
	if err != nil {
		return "", err
	}
	image, err := normalizeImageResult(result)
	if err != nil {
		return "", fmt.Errorf("解析生图结果失败: %w", err)
	}
	return image, nil
}

func buildMangaBriefPrompt(ts []TopicResult, users map[int64]UserImage) (string, error) {
	ids := make([]int64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	characters := make([]mangaSourceUser, 0, len(ids))
	for _, id := range ids {
		u := users[id]
		characters = append(characters, mangaSourceUser{ID: u.Id, NickName: u.NickName, Mbti: u.Mbti, Title: u.Title, Reason: u.Reason, GroupTitle: u.GroupTitle, Sex: u.Sex})
	}
	topics := make([]mangaSourceTopic, 0, len(ts))
	for i, topic := range ts {
		topics = append(topics, mangaSourceTopic{Index: i + 1, Title: topic.Topic, ContributorIDs: topic.Contributors, Detail: topic.Detail})
	}

	characterJSON, err := json.Marshal(characters)
	if err != nil {
		return "", fmt.Errorf("序列化漫画人物失败: %w", err)
	}
	topicJSON, err := json.Marshal(topics)
	if err != nil {
		return "", fmt.Errorf("序列化漫画话题失败: %w", err)
	}
	return fmt.Sprintf(mangaBriefPrompt, len(topics), characterJSON, topicJSON), nil
}

func normalizeMangaBrief(brief *mangaBrief, ts []TopicResult, users map[int64]UserImage) error {
	if len(brief.Characters) > 5 {
		return fmt.Errorf("人物数量为%d，最多允许5人", len(brief.Characters))
	}
	if len(users) > 0 && len(brief.Characters) == 0 {
		return fmt.Errorf("人物列表为空")
	}
	selected := make(map[int64]mangaBriefCharacter, len(brief.Characters))
	for i := range brief.Characters {
		character := &brief.Characters[i]
		u, ok := users[character.ID]
		if !ok {
			return fmt.Errorf("人物id %d不在候选列表中", character.ID)
		}
		if _, duplicate := selected[character.ID]; duplicate {
			return fmt.Errorf("人物id %d重复", character.ID)
		}
		character.NickName = u.NickName
		selected[character.ID] = *character
	}
	if len(brief.Topics) != len(ts) {
		return fmt.Errorf("话题数量为%d，期望%d", len(brief.Topics), len(ts))
	}
	for i := range brief.Topics {
		topic := &brief.Topics[i]
		if topic.Index != i+1 {
			return fmt.Errorf("第%d个话题的index为%d", i+1, topic.Index)
		}
		topic.Title = ts[i].Topic
		allowed := make(map[int64]struct{}, len(ts[i].Contributors))
		for _, id := range ts[i].Contributors {
			allowed[id] = struct{}{}
		}
		seen := make(map[int64]struct{}, len(topic.ParticipantIDs))
		topic.Participants = make([]string, 0, len(topic.ParticipantIDs))
		for _, id := range topic.ParticipantIDs {
			character, chosen := selected[id]
			_, contributor := allowed[id]
			if !chosen || !contributor {
				return fmt.Errorf("话题%d包含无效参与者id %d", i+1, id)
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			topic.Participants = append(topic.Participants, fmt.Sprintf("%d:%s", id, character.NickName))
		}
	}
	return nil
}

func buildMangaPrompt(brief mangaBrief, style string) (string, error) {
	style = strings.TrimSpace(style)
	if style == "" {
		style = defaultMangaStyle
	}
	characterJSON, err := json.Marshal(brief.Characters)
	if err != nil {
		return "", fmt.Errorf("序列化漫画人物简报失败: %w", err)
	}
	// 生图模型只接收最终绘制所需字段，不再重复传文本阶段使用的participant_ids。
	topics := make([]mangaImageTopic, 0, len(brief.Topics))
	for _, topic := range brief.Topics {
		topics = append(topics, mangaImageTopic{
			Index: topic.Index, Title: topic.Title, Participants: topic.Participants, Summary: topic.Summary,
		})
	}
	topicJSON, err := json.Marshal(topics)
	if err != nil {
		return "", fmt.Errorf("序列化漫画分镜简报失败: %w", err)
	}
	return fmt.Sprintf(mangaPrompt, style, len(brief.Topics), characterJSON, topicJSON), nil
}

func normalizeImageResult(result string) (string, error) {
	result = strings.TrimSpace(result)
	if result == "" {
		return "", fmt.Errorf("生图模型返回了空内容")
	}
	if match := markdownImagePattern.FindStringSubmatch(result); len(match) == 2 {
		return normalizeImageSource(match[1])
	}
	if match := httpImagePattern.FindString(result); match != "" {
		return strings.TrimRight(match, ".,;，。；)】]"), nil
	}
	return normalizeImageSource(strings.Trim(result, "`\"'"))
}

func normalizeImageSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "base64://") {
		return source, nil
	}
	if comma := strings.Index(source, ","); strings.HasPrefix(source, "data:image/") && comma >= 0 {
		if !strings.Contains(source[:comma], ";base64") {
			return "", fmt.Errorf("不支持非 base64 的 data URL")
		}
		payload := strings.TrimSpace(source[comma+1:])
		if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
			return "", fmt.Errorf("图片 base64 无效: %w", err)
		}
		return "base64://" + payload, nil
	}
	return "", fmt.Errorf("未找到图片 URL 或 base64 图片")
}
