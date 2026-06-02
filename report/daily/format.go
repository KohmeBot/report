package daily

import (
	"fmt"
	"strings"
	"time"
)

func formatTime(t time.Time) string {
	return t.Format("01-02 15时")
}

func formatMessages(msgs []GroupMessage, ump map[int64]User) string {
	var builder strings.Builder
	for _, msg := range msgs {
		str := formatMessage(msg, ump)
		if str == "" {
			continue
		}
		builder.WriteString(str)
		builder.WriteString("\n")
	}
	return builder.String()

}

func formatMessage(msg GroupMessage, ump map[int64]User) string {
	var builder strings.Builder
	u := ump[msg.UserID]

	target := "(某人)"
	tu, hasTarget := ump[msg.TargetUserID]
	if hasTarget {
		target = fmt.Sprintf("(%d)", tu.UserId)
	}

	var content string
	var action string
	switch msg.MsgType {
	case MsgTypeText:
		action = "说"
	case MsgTypeImg:
		action = "发了一张图或表情包"
	case MsgTypeAt:

		action = fmt.Sprintf("@%s 说", target)
	case MsgTypePoke:

		action = fmt.Sprintf("戳了戳%s", target)
	case MsgTypeReply:

		action = fmt.Sprintf("回复%s", target)
	case MsgTypeForward:
		action = "转了一条消息(搬屎)"
	case MsgTypeRecord:
		action = "发了条语音"
	case MsgTypeJson:
		action = "分享了一条内容(搬屎)"
	}
	if action == "" {
		return ""
	}
	content = msg.Content
	if runeLen(content) > 100 {
		// 限制100字
		content = string([]rune(content)[:100]) + "..."
	}
	// [5-15 11:11] 某某: XXX
	builder.WriteString(fmt.Sprintf("[%s] (%d) [%s]", msg.CreatedAt.Format("01-02 15:04"), u.UserId, action))
	if content != "" {
		builder.WriteString(fmt.Sprintf(": %s", content))
	}
	return builder.String()
}
