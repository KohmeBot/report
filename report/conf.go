package report

import "strings"

type Config struct {
	// 定时发送的群
	SendGroups []int64 `yaml:"send_groups" jsonschema:"description=定时发送的群"`

	// 是否纯文本
	OnlyText bool `yaml:"only_text" jsonschema:"description=是否纯文本"`

	// ChromeWs 地址,用于渲染图片，如果地址为空，则使用纯文本生成
	ChromeWs string `yaml:"chrome_ws" jsonschema:"description=ChromeWs 地址|用于渲染图片|如果地址为空|则使用纯文本生成"`

	// 在生成时是否开启深度思考,会加大token使用
	Thinking bool `yaml:"thinking" jsonschema:"description=在生成时是否开启深度思考|会加大token使用"`

	// 是否开启联网搜索
	Online bool `yaml:"online" jsonschema:"description=是否开启联网搜索"`

	// 日报标题
	Title string `yaml:"title" jsonschema:"description=日报标题"`
}

func (c Config) ChromeAddr() string {
	addr := strings.TrimPrefix(c.ChromeWs, "ws://")
	return "ws://" + addr
}
