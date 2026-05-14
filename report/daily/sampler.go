package daily

import (
	"math/rand/v2"
	"strings"
	"time"
)

func filterMessages(msgs []GroupMessage) []GroupMessage {
	res := make([]GroupMessage, 0, len(msgs))
	// 只取文本类消息参与筛选
	for _, m := range msgs {
		if m.MsgType != MsgTypeText || m.Content == "" {
			continue
		}
		// 像网页链接之类的不应该作为代表发言
		if strings.Contains(m.Content, "http://") {
			continue
		}
		if strings.Contains(m.Content, "https://") {
			continue
		}
		res = append(res, m)
	}
	return res

}

// SampleMessages 从一个人的所有发言里筛出最有代表性的4条
// msgs 必须按 CreatedAt 升序
func SampleMessages(msgs []GroupMessage) []GroupMessage {
	if len(msgs) == 0 {
		return nil
	}

	// 清洗
	textMsgs := filterMessages(msgs)

	if len(textMsgs) == 0 {
		return nil
	}
	if len(textMsgs) <= 4 {
		return textMsgs
	}

	result := make([]GroupMessage, 0, 4)
	used := make(map[uint]bool) // key 用消息ID，唯一且稳定

	pickMsg := func(fn func([]GroupMessage, map[uint]bool) int) {
		idx := fn(textMsgs, used)
		if idx >= 0 {
			result = append(result, textMsgs[idx])
			used[textMsgs[idx].ID] = true
		}
	}

	// 策略1：最长的一条（最有信息量）
	pickMsg(pickLongest)

	// 策略2：情绪最强烈（感叹号+问号最多）
	pickMsg(pickMostEmotional)

	// 策略3：口头禅（前5字相同且重复最多的句式的代表句）
	pickMsg(pickMostRepresentative)

	// 策略4：时间上最孤立（距离自己前后发言间隔最长，真实分钟数）
	pickMsg(pickMostIsolated)

	// 兜底：不足4条则从剩余消息中随机补齐
	if len(result) < 4 {
		// 收集所有未used的消息下标
		remaining := make([]int, 0)
		for i, m := range textMsgs {
			if !used[m.ID] {
				remaining = append(remaining, i)
			}
		}
		// 随机打乱
		rand.Shuffle(len(remaining), func(i, j int) {
			remaining[i], remaining[j] = remaining[j], remaining[i]
		})
		// 补齐到4条
		for _, idx := range remaining {
			if len(result) >= 4 {
				break
			}
			result = append(result, textMsgs[idx])
			used[textMsgs[idx].ID] = true
		}
	}

	return result
}

func pickLongest(msgs []GroupMessage, used map[uint]bool) int {
	maxLen, idx := 0, -1
	for i, m := range msgs {
		if used[m.ID] {
			continue
		}
		if l := runeLen(m.Content); l > maxLen {
			maxLen = l
			idx = i
		}
	}
	return idx
}

func pickMostEmotional(msgs []GroupMessage, used map[uint]bool) int {
	maxScore, idx := -1, -1
	for i, m := range msgs {
		if used[m.ID] {
			continue
		}
		score := strings.Count(m.Content, "!") + strings.Count(m.Content, "！") +
			strings.Count(m.Content, "?") + strings.Count(m.Content, "？")
		if score > maxScore {
			maxScore = score
			idx = i
		}
	}
	if maxScore == 0 {
		return -1
	}
	return idx
}

func pickMostRepresentative(msgs []GroupMessage, used map[uint]bool) int {
	freq := make(map[string][]int)
	for i, m := range msgs {
		if used[m.ID] {
			continue
		}
		r := []rune(m.Content)
		keyLen := 5
		if len(r) < keyLen {
			keyLen = len(r)
		}
		if keyLen < 2 {
			continue
		}
		key := string(r[:keyLen])
		freq[key] = append(freq[key], i)
	}

	bestIdx := -1
	bestCount := 1
	for _, idxList := range freq {
		if len(idxList) > bestCount {
			bestCount = len(idxList)
			// 取这组里最长的作为代表
			best, maxLen := -1, 0
			for _, i := range idxList {
				if l := runeLen(msgs[i].Content); l > maxLen {
					maxLen = l
					best = i
				}
			}
			bestIdx = best
		}
	}
	return bestIdx
}

func pickMostIsolated(msgs []GroupMessage, used map[uint]bool) int {
	maxGap, idx := time.Duration(0), -1

	for i, m := range msgs {
		if used[m.ID] {
			continue
		}

		// 找前一条未used消息的时间
		prevGap := time.Duration(0)
		for j := i - 1; j >= 0; j-- {
			if !used[msgs[j].ID] {
				prevGap = m.CreatedAt.Sub(msgs[j].CreatedAt)
				break
			}
		}

		// 找后一条未used消息的时间
		nextGap := msgs[len(msgs)-1].CreatedAt.Sub(msgs[0].CreatedAt)
		for j := i + 1; j < len(msgs); j++ {
			if !used[msgs[j].ID] {
				nextGap = msgs[j].CreatedAt.Sub(m.CreatedAt)
				break
			}
		}

		gap := prevGap + nextGap
		if gap > maxGap {
			maxGap = gap
			idx = i
		}
	}

	// 前后间隔加起来不足20分钟，说明发言密集，没有孤立感
	if maxGap < 20*time.Minute {
		return -1
	}
	return idx
}
