package daily

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"regexp"
	"slices"
	"strings"

	"github.com/kohmebot/report/report/invoker"
	zero "github.com/wdvxdr1123/ZeroBot"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)`)
	httpImagePattern     = regexp.MustCompile(`https?://[^\s<>"']+`)
)

const mangaPrompt = `请根据下方真实的群聊话题和人物资料，生成一张完整的竖版彩色趣味群聊漫画。

版式要求：
1. 所有内容必须在同一张图片中，整体像一页精致的中文网络条漫。
2. 图片顶部先放“登场人物”区域，为所有人物画简洁角色卡：角色外观要结合性别、MBTI、群头衔、昨日称号及称号原因设计，并清楚标注昵称。不要展示用户ID。
3. 人物介绍之后画恰好 %d 个场景分镜，一个话题对应一个分镜，不得合并或遗漏。
4. 每个分镜顶部必须原样、清晰地写出该话题的标题；标题不要改写。
5. 同一人物在人物介绍和所有分镜中的发型、服装、配色及面部特征必须保持一致。每格只安排该话题的参与者。
6. 根据话题详情设计有戏剧性、有梗但友善的动作、表情、场景和少量对话。允许夸张演绎气氛，不得捏造新的真实事件或改变原话题结论。
7. 重点保证中文昵称和分镜标题准确可读；避免大段文字、密集说明、平台水印和额外的总结区。

视觉风格：轻松活泼、表情夸张、线条清晰、色彩明快、分镜边界明确，适合直接发送到群聊中阅读。

人物资料 JSON：
%s

话题分镜 JSON（顺序就是分镜顺序）：
%s

请直接生成图片，只返回最终图片，不要返回创作说明。`

type UserImage struct {
	// 用户ID
	Id int64 `json:"id,omitempty"`
	// 用户昵称
	NickName string `json:"nickname,omitempty"`
	// MBTI
	Mbti string `json:"mbti,omitempty"`
	// 用户昨天获得的称号
	Title string `json:"title,omitempty"`
	// 用户获得称号原因
	Reason string `json:"reason,omitempty"`
	// 群头衔
	GroupTitle string `json:"groupTitle,omitempty"`
	// 性别 male 或 female 或 unknown
	Sex string `json:"sex,omitempty"`
}

type mangaUser struct {
	ID         int64  `json:"id"`
	NickName   string `json:"nickname"`
	Mbti       string `json:"mbti,omitempty"`
	Title      string `json:"title,omitempty"`
	Reason     string `json:"reason,omitempty"`
	GroupTitle string `json:"groupTitle,omitempty"`
	Sex        string `json:"sex,omitempty"`
}

type mangaTopic struct {
	Title        string   `json:"title"`
	Participants []string `json:"participants"`
	Detail       string   `json:"detail"`
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

			img := UserImage{
				Id:         uid,
				NickName:   nickName,
				Mbti:       "",
				Title:      "",
				Reason:     "",
				GroupTitle: memberInfo.Get("title").String(),
				Sex:        memberInfo.Get("sex").String(),
			}

			for _, u := range us {
				if u.User == uid {
					img.Mbti = u.Mbti
					img.Title = u.Title
					img.Reason = u.Reason
					break
				}
			}
			res[uid] = img
		}
	})

	logrus.Infof("用户印象: %v", res)

	return res, nil

}

// BuildManga 把全部话题组织成同一张“人物介绍 + N 格分镜”漫画。
// ImageInvoker 的不同后端可能返回裸 URL、Markdown 图片或 data URL，返回前统一转换为 OneBot 可发送的图片地址。
func (g *Generator) BuildManga(ts []TopicResult, users map[int64]UserImage) (string, error) {
	if len(ts) == 0 {
		return "", nil
	}

	prompt, err := buildMangaPrompt(ts, users)
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

func buildMangaPrompt(ts []TopicResult, users map[int64]UserImage) (string, error) {
	ids := make([]int64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	characters := make([]mangaUser, 0, len(ids))
	for _, id := range ids {
		u := users[id]
		characters = append(characters, mangaUser{
			ID:         u.Id,
			NickName:   u.NickName,
			Mbti:       u.Mbti,
			Title:      u.Title,
			Reason:     u.Reason,
			GroupTitle: u.GroupTitle,
			Sex:        u.Sex,
		})
	}

	topics := make([]mangaTopic, 0, len(ts))
	for _, topic := range ts {
		participants := make([]string, 0, len(topic.Contributors))
		for _, uid := range topic.Contributors {
			if u, ok := users[uid]; ok {
				participants = append(participants, u.NickName)
			}
		}
		topics = append(topics, mangaTopic{
			Title:        topic.Topic,
			Participants: participants,
			Detail:       topic.Detail,
		})
	}

	characterJSON, err := json.MarshalIndent(characters, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化漫画人物失败: %w", err)
	}
	topicJSON, err := json.MarshalIndent(topics, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化漫画话题失败: %w", err)
	}

	return fmt.Sprintf(mangaPrompt, len(topics), characterJSON, topicJSON), nil
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
