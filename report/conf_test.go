package report

import "testing"

func TestMangaConfigEnabledFor(t *testing.T) {
	tests := []struct {
		name  string
		conf  MangaConfig
		group int64
		want  bool
	}{
		{name: "disabled", conf: MangaConfig{}, group: 1, want: false},
		{name: "all groups", conf: MangaConfig{Enabled: true}, group: 1, want: true},
		{name: "selected group", conf: MangaConfig{Enabled: true, Groups: []int64{1, 2}}, group: 2, want: true},
		{name: "unselected group", conf: MangaConfig{Enabled: true, Groups: []int64{1, 2}}, group: 3, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.conf.EnabledFor(tt.group); got != tt.want {
				t.Fatalf("EnabledFor(%d) = %v, want %v", tt.group, got, tt.want)
			}
		})
	}
}
