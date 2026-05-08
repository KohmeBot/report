package daily

import (
	"math/rand"
	"sort"
	"strings"
)

// SampleMessages 从一个人的所有发言里筛出最有代表性的4条
func SampleMessages(contents []string) []string {
	if len(contents) == 0 {
		return nil
	}
	if len(contents) <= 4 {
		return contents
	}

	result := make([]string, 0, 4)
	used := make(map[int]bool)

	// 策略1：最长的一条（最有信息量）
	longest := pickLongest(contents)
	if longest >= 0 {
		result = append(result, contents[longest])
		used[longest] = true
	}

	// 策略2：包含问号最多的一条（最迷茫/最好奇）
	mostQuestion := pickMostQuestion(contents, used)
	if mostQuestion >= 0 {
		result = append(result, contents[mostQuestion])
		used[mostQuestion] = true
	}

	// 策略3：最短且不是纯标点/表情的一条（最懒的发言）
	shortest := pickMeaningfulShortest(contents, used)
	if shortest >= 0 {
		result = append(result, contents[shortest])
		used[shortest] = true
	}

	// 策略4：随机一条兜底（保留随机性）
	for i := 0; i < 20; i++ {
		idx := rand.Intn(len(contents))
		if !used[idx] {
			result = append(result, contents[idx])
			break
		}
	}

	return result
}

func pickLongest(contents []string) int {
	maxLen, idx := 0, -1
	for i, s := range contents {
		if l := runeLen(s); l > maxLen {
			maxLen = l
			idx = i
		}
	}
	return idx
}

func pickMostQuestion(contents []string, used map[int]bool) int {
	maxQ, idx := -1, -1
	for i, s := range contents {
		if used[i] {
			continue
		}
		q := strings.Count(s, "?") + strings.Count(s, "？")
		if q > maxQ {
			maxQ = q
			idx = i
		}
	}
	if maxQ == 0 {
		return -1 // 没有问句就不取
	}
	return idx
}

func pickMeaningfulShortest(contents []string, used map[int]bool) int {
	// 过滤掉纯标点、纯数字、只有表情的
	candidates := make([]int, 0)
	for i, s := range contents {
		if used[i] {
			continue
		}
		stripped := strings.TrimFunc(s, func(r rune) bool {
			return strings.ContainsRune("哈呵嗯啊哦噢。，！？.,!? \t", r)
		})
		if runeLen(stripped) >= 2 { // 至少有2个有意义的字
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return -1
	}
	// 取最短的
	sort.Slice(candidates, func(a, b int) bool {
		return runeLen(contents[candidates[a]]) < runeLen(contents[candidates[b]])
	})
	return candidates[0]
}
