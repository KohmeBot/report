package daily

import "time"

const (
	softLimit = 400 // 超过这个数量开始压缩
	hardLimit = 600 // 超过这个数量强制采样
)

// findHotSegment 找消息最多的自然段
// 如果最热的段太短，尝试合并相邻段
// findHotSegment 找消息最多的自然段，并控制消息数量防止token爆炸
func findHotSegment(msgs []GroupMessage) HotPeriod {
	if len(msgs) == 0 {
		return HotPeriod{}
	}

	// 第一次尝试：15分钟停顿
	segments := splitByPause(msgs, 15*time.Minute)
	hot := pickHottest(segments)

	return *hot
}

// splitByPause 按自然停顿切割消息流
// pauseThreshold: 超过这个间隔就认为话题断了
func splitByPause(msgs []GroupMessage, pauseThreshold time.Duration) []ChatSegment {
	if len(msgs) == 0 {
		return nil
	}

	segments := make([]ChatSegment, 0)
	current := ChatSegment{
		Start:    msgs[0].CreatedAt,
		Messages: []GroupMessage{msgs[0]},
	}

	for i := 1; i < len(msgs); i++ {
		gap := msgs[i].CreatedAt.Sub(msgs[i-1].CreatedAt)
		if gap >= pauseThreshold {
			// 话题断了，保存当前段，开始新段
			current.End = msgs[i-1].CreatedAt
			segments = append(segments, current)
			current = ChatSegment{
				Start:    msgs[i].CreatedAt,
				Messages: []GroupMessage{msgs[i]},
			}
		} else {
			current.Messages = append(current.Messages, msgs[i])
		}
	}
	// 最后一段
	current.End = msgs[len(msgs)-1].CreatedAt
	segments = append(segments, current)

	return segments
}

// compressMessages 控制消息数量，防止token爆炸
func compressMessages(msgs []GroupMessage) []GroupMessage {
	count := len(msgs)

	// 数量正常，直接返回
	if count <= softLimit {
		return msgs
	}

	// 去重压缩
	// 策略：合并同一人连续发的短句，过滤纯表情/图片
	if count <= hardLimit {
		return deduplicateMessages(msgs)
	}

	// 超过120条：去重后再采样
	deduped := deduplicateMessages(msgs)
	if len(deduped) <= softLimit {
		return deduped
	}
	return sampleMessages(deduped, softLimit)
}

// deduplicateMessages 合并连续短句，过滤低信息量消息
func deduplicateMessages(msgs []GroupMessage) []GroupMessage {
	result := make([]GroupMessage, 0, len(msgs))

	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]

		// 过滤纯图片/表情包/戳一戳（对摘要没有帮助）
		if msg.MsgType == MsgTypePoke || msg.MsgType == MsgTypeImg {
			continue
		}

		// 合并同一人60秒内的连续短句（5字以内）
		if len(result) > 0 {
			prev := &result[len(result)-1]
			gap := msg.CreatedAt.Sub(prev.CreatedAt)
			if prev.UserID == msg.UserID &&
				gap <= 60*time.Second &&
				runeLen(prev.Content) <= 5 &&
				runeLen(msg.Content) <= 5 {
				// 把这条短句追加到上一条，不单独列
				prev.Content = prev.Content + msg.Content
				continue
			}
		}

		result = append(result, msg)
	}

	return result
}

// sampleMessages 保头保尾均匀采样，保留对话的起伏感
func sampleMessages(msgs []GroupMessage, limit int) []GroupMessage {
	if len(msgs) <= limit {
		return msgs
	}

	// 头部保留20%，尾部保留20%，中间均匀采样
	headSize := limit / 5
	tailSize := limit / 5
	midSize := limit - headSize - tailSize

	head := msgs[:headSize]
	tail := msgs[len(msgs)-tailSize:]
	mid := msgs[headSize : len(msgs)-tailSize]

	// 中间部分均匀采样
	sampled := make([]GroupMessage, 0, midSize)
	step := float64(len(mid)) / float64(midSize)
	for i := 0; i < midSize; i++ {
		idx := int(float64(i) * step)
		if idx < len(mid) {
			sampled = append(sampled, mid[idx])
		}
	}

	result := make([]GroupMessage, 0, limit)
	result = append(result, head...)
	result = append(result, sampled...)
	result = append(result, tail...)
	return result
}

// pickHottest 找消息最多的段，如果最热段太短则尝试合并相邻段
func pickHottest(segments []ChatSegment) *HotPeriod {
	if len(segments) == 0 {
		return nil
	}

	// 找消息最多的段的下标
	bestIdx := 0
	for i, seg := range segments {
		if len(seg.Messages) > len(segments[bestIdx].Messages) {
			bestIdx = i
		}
	}

	best := segments[bestIdx]

	// 如果相邻段时间很近（不超过20分钟），合并进来
	// 向前合并
	if bestIdx > 0 {
		prev := segments[bestIdx-1]
		gap := best.Start.Sub(prev.End)
		if gap <= 20*time.Minute {
			merged := append(prev.Messages, best.Messages...)
			best = ChatSegment{
				Start:    prev.Start,
				End:      best.End,
				Messages: merged,
			}
		}
	}

	// 向后合并
	if bestIdx < len(segments)-1 {
		next := segments[bestIdx+1]
		gap := next.Start.Sub(best.End)
		if gap <= 20*time.Minute {
			merged := append(best.Messages, next.Messages...)
			best = ChatSegment{
				Start:    best.Start,
				End:      next.End,
				Messages: merged,
			}
		}
	}

	return &HotPeriod{
		Start: best.Start,
		End:   best.End,
	}
}
