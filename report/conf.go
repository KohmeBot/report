package report

import (
	"slices"
	"strings"
)

type MangaConfig struct {
	// 是否生成群聊漫画
	Enabled bool `yaml:"enabled" jsonschema:"description=是否在日报后生成并发送群聊漫画"`

	// 启用漫画的群，留空代表全部群
	Groups []int64 `yaml:"groups" jsonschema:"description=启用漫画的群|留空代表全部群"`

	ProviderName string `yaml:"provider_name" jsonschema:"description=生图模型供应商"`
	ModelName    string `yaml:"model_name" jsonschema:"description=生图模型名称"`
}

func (c MangaConfig) EnabledFor(group int64) bool {
	return c.Enabled && (len(c.Groups) == 0 || slices.Contains(c.Groups, group))
}

type Config struct {
	ProviderName string `yaml:"provider_name" jsonschema:"description=模型供应商"`
	ModelName    string `yaml:"model_name" jsonschema:"description=模型名称"`

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

	// 每日发送的时间
	SendTime string `yaml:"send_time" jsonschema:"default=09:00,description=每天定时发送日报的时间，例如09:00"`

	// 最低消息数
	ReportMinMessageCount int64 `yaml:"report_min_message_count" jsonschema:"description=最低消息数，群消息只有高于这个数才会生成日报"`

	// 群聊漫画配置
	Manga MangaConfig `yaml:"manga" jsonschema:"description=群聊漫画配置"`
}

func (c Config) ChromeAddr() string {
	addr := strings.TrimPrefix(c.ChromeWs, "ws://")
	return "ws://" + addr
}
