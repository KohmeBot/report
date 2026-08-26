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

const mangaPrompt = `根据人物资料和话题生成一张完整竖版彩色中文Q版群聊漫画。

【人物】
- 人物JSON是唯一角色表；以id唯一识别，同一人全图必须保持同一造型。
- 先为每人固定发型、发色、脸型、服装、主色和明显特征；顶部角色卡就是后续分镜的唯一标准，不得换脸、换发型、换衣服或换配色。
- 不同人物必须一眼可区分，禁止模板脸。任意两人至少在发型、发色、脸型、眼镜/帽子、服装类型、主色、配件中有3项明显不同；即使遮住昵称也应看出不是同一人。
- 不得创建人物JSON之外的角色。

【版式】
- 顶部为“登场人物”，每人恰好1张角色卡，只写昵称。
- 之后恰好%d个分镜，每个话题1格，顺序不变、不合并、不遗漏。
- 每格标题必须原样写出，只出现该话题参与者。

【漫画表现】
- 每格必须有1~3处简短对白、吐槽、旁白或拟声词，不能只有标题；单条尽量4~14字，多人话题优先让至少2人开口。
- 台词从话题内容提炼，不捏造事件或改变结论。
- 表情动作夸张有趣，可用汗滴、问号、爱心、速度线等漫画符号。

【风格】
萌系日系Q版二维彩色条漫，2.5~3头身，大头小身，深色圆润勾线、平涂+轻赛璐璐阴影，明快柔和；不要写实、3D、真人感。

人物JSON：
%s

话题JSON：
%s

严格复用角色并保证中文标题、昵称和对白清晰可读。直接生成图片，不输出说明。`

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
