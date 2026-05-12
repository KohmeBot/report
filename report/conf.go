package report

type Config struct {
	// 定时发送的群
	SendGroups []int64 `yaml:"send_groups"`
	// 是否重新生成主题
	RegenTheme bool `yaml:"regen_theme"`
}
