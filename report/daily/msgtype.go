package daily

import "slices"

const (
	MsgTypeText    = "text"
	MsgTypeImg     = "image"
	MsgTypeAt      = "at"
	MsgTypePoke    = "poke"
	MsgTypeReply   = "reply"
	MsgTypeForward = "forward"
	MsgTypeRecord  = "record"
	MsgTypeJson    = "json"
)

func HasMsgType(typ string) bool {
	return slices.Contains([]string{
		MsgTypeText,
		MsgTypeImg,
		MsgTypeAt,
		MsgTypePoke,
		MsgTypeReply,
		MsgTypeForward,
		MsgTypeRecord,
		MsgTypeJson,
	}, typ)
}

func MsgTypeString(typ string) string {
	switch typ {
	case MsgTypeText:
		return "文本"
	case MsgTypeImg:
		return "图片或表情包"
	case MsgTypeAt:
		return "At"
	case MsgTypePoke:
		return "戳一戳"
	case MsgTypeReply:
		return "回复"
	case MsgTypeForward:
		return "转发(搬屎)"
	case MsgTypeRecord:
		return "语音"
	case MsgTypeJson:
		return "分享内容(搬屎)"
	}
	return ""
}
