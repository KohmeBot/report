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

const defaultMangaStyle = "萌系日系Q版二维彩色漫画，2.5~3头身，大头小身，深色圆润勾线，平涂加轻赛璐璐阴影，明快柔和"

const mangaBriefRewriteAttempts = 3

const mangaBriefSystem = `你是一位想象力旺盛的漫画编剧、导演和分镜师。只返回符合要求的JSON，不要输出Markdown或解释。`

const mangaBriefPrompt = `把下面的人物与话题改编成一部有完整场景、强烈动作和视觉冲击力的短篇漫画，并输出可直接交给生图模型的导演简报。

要求：
1. characters最多5人。优先选择能覆盖更多话题、最适合推动故事的人；候选不超过5人时全部保留。
2. 每人的appearance只用一句短句描述可见外观：发型/发色、脸型或眼镜、服装和主色、一个显著配件。人物之间至少有3处明显不同，一眼能区分。
3. 你可以把不同topic大胆串成一个荒诞、有起承转合的小故事，可以编造转场、误会、道具、反应和喜剧冲突，但要保留每个topic的核心梗，不得歪曲其明确结论。
4. title是整部漫画醒目而有趣的总标题；story用几句话讲清故事主线；composition自由决定画布横竖比例、人物如何登场、分格数量与大小、跨格/破框/特写、阅读顺序、总标题/话题标题/人物昵称放在哪里。不要采用整齐角色卡或机械等高分格。
5. topics数量必须恰好为%d，index从1连续递增，顺序不变。每个topic是一个故事段落，但可以使用一个或多个分格。direction用2~4句写成具体的绘制指令，必须包括明确地点和环境、可见道具、人物在做什么、夸张表情或肢体动作、景别/视角，以及极短的中文对白或拟声词。
6. 每个段落必须发生在能被画出来的具体场景中，并让人物与环境或道具发生互动。禁止空白背景下几个人并排站着或坐着聊天，禁止全篇使用同一机位，至少安排一次大幅动作、视觉冲突、追逐、跌落、喷射、爆炸式反应或同等冲击力的漫画场面。
7. participant_ids只能使用入选characters的id。优先保留原话题参与者，也可以让其他入选角色跨topic追赶、围观、误入或承担转场，以便故事连贯；没有合适人物时返回空数组。
8. 不要决定画风、媒介或渲染质感，画风由外部提供；性别unknown时不要擅自确定性别。

输出JSON格式：
{
  "title": "整部漫画的总标题",
  "story": "串联全部话题的小故事主线",
  "composition": "自由大胆的整体版式、人物登场、标题昵称落点和镜头节奏",
  "characters": [{"id": 123, "appearance": "一句外观描述"}],
  "topics": [{"index": 1, "participant_ids": [123], "direction": "具体场景、动作、镜头、对白和拟声词"}]
}

原始人物JSON：
%s

原始话题JSON：
%s`

const mangaRetryPrompt = `

上一次输出选了%d个人，超过最多5人的硬限制。请彻底重写整份JSON，只保留最能串起全部话题的最多5人；不要仅解释或道歉。`

const mangaPrompt = `根据以下导演简报，画一部完整、有故事性和强烈场景感的中文漫画。

风格：%s。

创作原则：
- 严格执行导演简报的story与composition；画布横竖比例、分格数量大小、人物登场方式、标题和昵称位置均由简报决定，不要默认角色卡或整齐纵向排版。
- %d个topic是按顺序出现的故事段落，不等于固定%d格；一个段落可用一个或多个分格，也可跨格、破框、叠框或使用大幅主画面，但不得遗漏任何topic。
- 场景与动作优先。必须画出地点、环境、前中后景和关键道具，让人物奔跑、操作、争抢、跌倒、躲闪或与环境互动；禁止把几个人横排在空背景前轮流说话。
- 角色表是唯一人物来源；同一id始终保持相同外观，不添加表外人物。只在需要辨认时自然标注昵称。
- 中文总标题、话题标题、必要对白与拟声词要醒目清晰；对白精短，不要让大段文字挤占画面。

导演简报：
总标题：%s
故事主线：%s
整体构图：%s

角色表：
%s

故事段落：
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
	Title       string                `json:"title"`
	Story       string                `json:"story"`
	Composition string                `json:"composition"`
	Characters  []mangaBriefCharacter `json:"characters"`
	Topics      []mangaBriefTopic     `json:"topics"`
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
	Direction      string   `json:"direction"`
}

type mangaImageTopic struct {
	Index        int      `json:"index"`
	Title        string   `json:"title"`
	Participants []string `json:"participants"`
	Direction    string   `json:"direction"`
}

// IsEmpty 允许明确的空参与者数组；title 和 participants 由服务端补回。
func (t mangaBriefTopic) IsEmpty() bool {
	return t.Index <= 0 || t.ParticipantIDs == nil || strings.TrimSpace(t.Direction) == ""
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
	textInvoker := invoker.NewJsonInvoker(g.invoker, g.provider, g.model, mangaBriefSystem, false, false)
	brief, err := requestMangaBrief(briefRequest, ts, users, textInvoker.DoRequest)
	if err != nil {
		return "", err
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

type mangaBriefRequestFunc func(string, any) error

// requestMangaBrief 在首次超限后最多要求文本模型重写三次；仍超限时保留最后结果继续生图。
func requestMangaBrief(request string, ts []TopicResult, users map[int64]UserImage, doRequest mangaBriefRequestFunc) (mangaBrief, error) {
	currentRequest := request
	for attempt := 0; attempt <= mangaBriefRewriteAttempts; attempt++ {
		var brief mangaBrief
		if err := doRequest(currentRequest, &brief); err != nil {
			return mangaBrief{}, fmt.Errorf("生成漫画导演简报失败: %w", err)
		}

		tooManyCharacters := len(brief.Characters) > 5
		if tooManyCharacters && attempt < mangaBriefRewriteAttempts {
			logrus.Warnf("漫画导演简报返回%d人，要求文本模型第%d次重写", len(brief.Characters), attempt+1)
			currentRequest = request + fmt.Sprintf(mangaRetryPrompt, len(brief.Characters))
			continue
		}
		if tooManyCharacters {
			logrus.Warnf("漫画导演简报重写%d次后仍超过5人，保留最后的%d人结果继续生图", mangaBriefRewriteAttempts, len(brief.Characters))
		}
		if err := normalizeMangaBrief(&brief, ts, users, tooManyCharacters); err != nil {
			return mangaBrief{}, fmt.Errorf("漫画导演简报无效: %w", err)
		}
		return brief, nil
	}
	return mangaBrief{}, fmt.Errorf("生成漫画导演简报失败")
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

func normalizeMangaBrief(brief *mangaBrief, ts []TopicResult, users map[int64]UserImage, allowTooManyCharacters bool) error {
	if len(brief.Characters) > 5 && !allowTooManyCharacters {
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
		seen := make(map[int64]struct{}, len(topic.ParticipantIDs))
		topic.Participants = make([]string, 0, len(topic.ParticipantIDs))
		for _, id := range topic.ParticipantIDs {
			character, chosen := selected[id]
			if !chosen {
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
			Index: topic.Index, Title: topic.Title, Participants: topic.Participants, Direction: topic.Direction,
		})
	}
	topicJSON, err := json.Marshal(topics)
	if err != nil {
		return "", fmt.Errorf("序列化漫画分镜简报失败: %w", err)
	}
	return fmt.Sprintf(
		mangaPrompt,
		style,
		len(brief.Topics),
		len(brief.Topics),
		brief.Title,
		brief.Story,
		brief.Composition,
		characterJSON,
		topicJSON,
	), nil
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
