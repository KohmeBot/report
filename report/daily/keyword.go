package daily

import "strings"

var stopWords = map[string]bool{
	"的": true, "了": true, "是": true, "在": true, "我": true,
	"你": true, "他": true, "她": true, "它": true, "们": true,
	"这": true, "那": true, "有": true, "和": true, "就": true,
	"都": true, "也": true, "但": true, "还": true, "很": true,
	"不": true, "没": true, "好": true, "吗": true, "啊": true,
	"哦": true, "嗯": true, "哈": true, "哈哈": true, "哈哈哈": true,
	"呵呵": true, "嗯嗯": true, "好的": true, "对的": true,
	"然后": true, "所以": true, "因为": true, "如果": true,
}

// ExtractKeywords 从消息列表中提取topN高频词（2-4字）
func ExtractKeywords(contents []string, topN int) []KeywordStat {
	freq := make(map[string]int)

	for _, content := range contents {
		runes := []rune(content)
		// 滑动窗口提取2-4字的词组
		for size := 2; size <= 4; size++ {
			for i := 0; i+size <= len(runes); i++ {
				word := string(runes[i : i+size])
				if stopWords[word] {
					continue
				}
				// 过滤掉包含标点的
				if containsPunct(word) {
					continue
				}
				freq[word]++
			}
		}
	}

	// 转成切片排序
	stats := make([]KeywordStat, 0, len(freq))
	for word, count := range freq {
		if count >= 3 { // 至少出现3次才算关键词
			stats = append(stats, KeywordStat{Word: word, Count: count})
		}
	}

	// 按频次降序，取topN
	sortKeywords(stats)
	if len(stats) > topN {
		stats = stats[:topN]
	}
	return stats
}

func containsPunct(s string) bool {
	for _, r := range s {
		if strings.ContainsRune("。，！？.,!?、：:；;「」【】()（）\n\t ", r) {
			return true
		}
	}
	return false
}

func sortKeywords(stats []KeywordStat) {
	for i := 1; i < len(stats); i++ {
		for j := i; j > 0 && stats[j].Count > stats[j-1].Count; j-- {
			stats[j], stats[j-1] = stats[j-1], stats[j]
		}
	}
}
