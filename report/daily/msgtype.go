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
	}, typ)
}
