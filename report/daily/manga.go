package daily

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kohmebot/report/report/invoker"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
)

var (
	markdownImagePattern    = regexp.MustCompile(`!\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)`)
	httpImagePattern        = regexp.MustCompile(`https?://[^\s<>"']+`)
	mangaCharacterIDPattern = regexp.MustCompile(`人物ID\s*[:：=]\s*(\d+)`)
)

const defaultMangaStyle = "萌系日系Q版二维彩色漫画，2.5~3头身，大头小身，深色圆润勾线，平涂加轻赛璐璐阴影，明快柔和"
const mangaPreferredCharacterCount = 5
const mangaBriefMaxRunes = 800

const mangaBriefSystem = `你是一位想象力旺盛的漫画编剧、导演和分镜师。只输出完整漫画的自然语言绘制说明，不要输出JSON或Markdown代码块。`

const mangaBriefPrompt = `把下面的人物与话题改编成一部有完整场景、强烈动作和视觉冲击力的短篇漫画，并输出可直接交给生图模型的导演简报。

要求：
1. 人物没有硬性数量上限；根据故事需要选择人物，也可以使用全部候选人物。画面可读性允许时优先控制在5人以内，但超过5人也可以正常创作，不要为凑人数删掉故事需要的人物。
2. 【人物ID硬约束】人物只能来自下方“允许使用的人物ID白名单”。不得创造、猜测、修改，不得使用白名单之外的人物，也不得添加没有ID的路人、群众或新角色。
3. 为每位登场人物用一句短句描述可见外观：发型/发色、脸型或眼镜、服装和主色、一个显著配件。人物之间至少有3处明显不同，一眼能区分；同一人物全篇外观保持一致。
4. 你可以把不同话题大胆串成一个荒诞、有起承转合的小故事，可以编造转场、误会、道具、反应和喜剧冲突，但要保留每个话题的核心梗，不得歪曲其明确结论。只能编造情节和物件，绝对不能编造人物或人物ID。
5. 全部%d个话题按原顺序出现且一个不少。每个话题是一个故事段落，但可以使用一个或多个分格。具体说明明确地点和环境、可见道具、人物动作、夸张表情或肢体动作、景别/视角，以及极短的中文对白或拟声词。
6. 自由决定画布横竖比例、分格数量与大小、跨格/破框/特写、阅读顺序、总标题/话题标题/人物昵称放在哪里。不要采用整齐角色卡或机械等高分格。
7. 每个段落必须发生在能被画出来的具体场景中，并让人物与环境或道具发生互动。禁止空白背景下几个人并排站着或坐着聊天，禁止全篇使用同一机位，至少安排一次大幅动作、视觉冲突、追逐、跌落、喷射、爆炸式反应或同等冲击力的漫画场面。
8. 优先保留原话题参与者，也可以让白名单中的其他人物跨话题追赶、围观、误入或承担转场，以便故事连贯。不要决定画风、媒介或渲染质感；性别unknown时不要擅自确定性别。

请用约500字的自然语言完整描述漫画要怎么画，全文控制在450至550个字符，绝不能超过550个字符。精炼地写清总标题、故事主线、整体构图、登场人物及外观、每个话题的场景与分镜，不要解释创作过程，不要输出JSON。写完后自行核对每一个“人物ID”都来自以下白名单；如白名单为空，就不要安排任何人物出场。

允许使用的人物ID白名单（这是唯一可信的人物来源）：
%s

原始人物JSON：
%s

原始话题JSON：
%s`

const mangaPrompt = `画一部完整、有故事感的中文漫画。画风：%s。
严格执行简报，共%d个顺序话题，可自由分格但不能遗漏。场景、动作和道具优先，避免空背景站排；保持人物外观一致，文字精短清晰。
只允许画这些人物：%s。忽略简报中的其他人物、ID或路人。
导演简报：%s
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
	textInvoker := invoker.NewTextInvoker(g.invoker, g.provider, g.model, mangaBriefSystem, false, false)
	brief, err := requestMangaBrief(briefRequest, ts, users, textInvoker.DoRequest)
	if err != nil {
		return "", err
	}

	prompt, err := buildMangaPrompt(brief, g.mangaStyle, len(ts), users)
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

type mangaBriefRequestFunc func(string) (string, error)

func requestMangaBrief(request string, ts []TopicResult, users map[int64]UserImage, doRequest mangaBriefRequestFunc) (string, error) {
	brief, err := doRequest(request)
	if err != nil {
		return "", fmt.Errorf("生成漫画导演简报失败: %w", err)
	}
	return normalizeMangaBrief(brief, ts, users), nil
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
		characters = append(characters, mangaSourceUser{ID: id, NickName: u.NickName, Mbti: u.Mbti, Title: u.Title, Reason: u.Reason, GroupTitle: u.GroupTitle, Sex: u.Sex})
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
	return fmt.Sprintf(mangaBriefPrompt, len(topics), buildMangaCharacterWhitelist(users), characterJSON, topicJSON), nil
}

// normalizeMangaBrief 只记录文本简报中的可疑内容，不再因格式、人数或内容问题阻断生图。
func normalizeMangaBrief(brief string, ts []TopicResult, users map[int64]UserImage) string {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		logrus.Warn("漫画导演简报为空，仍继续请求生图")
		return brief
	}

	referencedIDs := make(map[int64]struct{})
	for _, match := range mangaCharacterIDPattern.FindAllStringSubmatch(brief, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			logrus.Warnf("漫画导演简报包含无法解析的人物ID %q，仍继续生图", match[1])
			continue
		}
		if _, ok := users[id]; !ok {
			logrus.Warnf("漫画导演简报包含白名单之外的人物ID %d，仍继续生图；生图提示词将要求忽略该人物", id)
		}
		referencedIDs[id] = struct{}{}
	}
	if len(referencedIDs) > mangaPreferredCharacterCount {
		logrus.Warnf("漫画导演简报引用了%d个人物，超过建议的%d人，但不做限制并继续生图", len(referencedIDs), mangaPreferredCharacterCount)
	}
	if len(users) > 0 && len(referencedIDs) == 0 {
		logrus.Warn("漫画导演简报没有使用规范的“人物ID:<id>”引用，仍继续生图")
	}
	if len(ts) == 0 {
		logrus.Warn("漫画导演简报没有对应话题，仍继续生图")
	}
	runes := []rune(brief)
	if len(runes) > mangaBriefMaxRunes {
		logrus.Warnf("漫画导演简报长度为%d个字符，超过%d字符；为适配生图模型上下文将截短", len(runes), mangaBriefMaxRunes)
		brief = string(runes[:mangaBriefMaxRunes])
	}
	return brief
}

func buildMangaCharacterWhitelist(users map[int64]UserImage) string {
	ids := make([]int64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		return "（空：不得安排任何人物出场）"
	}

	characters := make([]string, 0, len(ids))
	for _, id := range ids {
		characters = append(characters, fmt.Sprintf("人物ID:%d（%s）", id, users[id].NickName))
	}
	return strings.Join(characters, "、")
}

func buildReferencedMangaCharacterWhitelist(brief string, users map[int64]UserImage) string {
	seen := make(map[int64]struct{})
	characters := make([]string, 0)
	for _, match := range mangaCharacterIDPattern.FindAllStringSubmatch(brief, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			continue
		}
		u, ok := users[id]
		if !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		characters = append(characters, fmt.Sprintf("人物ID:%d（%s）", id, u.NickName))
	}
	if len(characters) == 0 {
		return "无（不得画人物）"
	}
	return strings.Join(characters, "、")
}

func buildMangaPrompt(brief, style string, topicCount int, users map[int64]UserImage) (string, error) {
	style = strings.TrimSpace(style)
	if style == "" {
		style = defaultMangaStyle
	}
	return fmt.Sprintf(
		mangaPrompt,
		style,
		topicCount,
		buildReferencedMangaCharacterWhitelist(brief, users),
		brief,
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
