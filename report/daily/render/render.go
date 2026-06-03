package render

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"fmt"
	"html"
	"html/template"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

//go:embed templates/*.html
var templateFS embed.FS

// View 是真正喂给根模板的视图模型：业务数据 + 一些渲染期附加信息。
type View struct {
	Data        *ReportData
	GeneratedAt string                                              // 生成时间，显示在尾注
	GeneratedBy string                                              // 由谁生成，显示在尾注
	GetUserData func(userID int64) (nickName string, avatar string) // 获取用户昵称和头像数据
	tmpl        *template.Template
}

// Option 用于配置渲染。
type Option func(*View)

// WithGeneratedAt 设置尾注里的生成时间。
func WithGeneratedAt(t string) Option { return func(v *View) { v.GeneratedAt = t } }

// WithGeneratedBy 设置尾注里的「由 XXX 生成」。
func WithGeneratedBy(by string) Option { return func(v *View) { v.GeneratedBy = by } }

func WithUserDataGetter(fn func(userID int64) (nickName string, avatar string)) Option {
	return func(v *View) {
		v.GetUserData = fn
	}
}

// Render 把一份 ReportData 渲染成完整的 HTML（写入 w）。
// 任意板块缺数据都会安全降级，输出为纯静态结构，适合再转成图片。
func Render(w io.Writer, data *ReportData, opts ...Option) error {
	v := &View{
		Data:        data,
		GeneratedBy: "群日报小助手",
		GetUserData: func(userID int64) (nickName string, avatar string) {
			return
		},
	}

	v.tmpl = template.Must(
		template.New("report").Funcs(v.funcMap()).ParseFS(templateFS, "templates/*.html"),
	)

	if v.Data == nil {
		v.Data = &ReportData{}
	}
	for _, o := range opts {
		o(v)
	}

	if v.Data.GroupQuality != nil {
		// 降序排序
		slices.SortFunc(v.Data.GroupQuality.Dimensions, func(a, b Dimension) int {
			return cmp.Compare(b.Percentage, a.Percentage)
		})
	}

	return v.tmpl.ExecuteTemplate(w, "report", v)
}

// RenderToString 是 Render 的便捷版本，直接返回 HTML 字符串。
func RenderToString(data *ReportData, opts ...Option) (string, error) {
	var buf bytes.Buffer
	if err := Render(&buf, data, opts...); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderToImage 把一份 ReportData 渲染成图片。
func RenderToImage(data *ReportData, chromeAddr string, opts ...Option) ([]byte, error) {
	htmlData, err := RenderToString(data, opts...)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if chromeAddr != "" {
		ctx, cancel = chromedp.NewRemoteAllocator(ctx, chromeAddr)
		defer cancel()
	}
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var imgBuf []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(440, 1000, chromedp.EmulateScale(3)),
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frameTree.Frame.ID, htmlData).Do(ctx)
		}),
		chromedp.WaitReady("body"),
		chromedp.FullScreenshot(&imgBuf, 100),
	)
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}
	return imgBuf, nil
}

// ───────────────────────── 模板函数 ─────────────────────────

func (v *View) funcMap() template.FuncMap {
	return template.FuncMap{
		"axisLabels": v.axisLabels,
		"avatar":     v.avatarHTML,   // 任意尺寸头像（img 或首字母兜底）
		"userChip":   v.userChipHTML, // 行内「头像+昵称」胶囊
		"detail":     v.detailHTML,   // 话题详情：替换 [用户ID] 为胶囊
		"reason":     v.reasonHTML,   // 金句原因：替换 [用户ID] 为胶囊
		"mod":        func(a, b int) int { return a % b },
		"add1":       func(i int) int { return i + 1 },
		"maxCount":   maxCount,  // 柱状图最大值
		"barHeight":  barHeight, // 单柱高度百分比
		"isPeak":     func(c, max int) bool { return max > 0 && c == max },
		"pct":        pct, // 百分比去尾零
		"hasAny":     func(s string) bool { return strings.TrimSpace(s) != "" },
		"avatarName": v.displayName, // 兜底昵称
	}
}

// 兜底头像配色盘
var avatarPalette = []string{
	"#FF7A59", "#5FC9A0", "#5BB8E8", "#A78BE0",
	"#FF8FB3", "#FFB03B", "#E0623F", "#3FA882",
}

func (v *View) axisLabels(slots []HourSlot) []int {
	labels := make([]int, 0, (len(slots)+2)/3)
	for i := 0; i < len(slots); i += 3 {
		labels = append(labels, slots[i].Hour)
	}
	return labels
}

func avatarColor(u int64) string {
	key := strconv.FormatInt(u, 10)
	h := 0
	for _, r := range key {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return avatarPalette[h%len(avatarPalette)]
}

func firstRune(s string) string {
	for _, r := range strings.TrimSpace(s) {
		return string(r)
	}
	return "?"
}

func (v *View) displayName(u int64) string {
	nickName, _ := v.GetUserData(u)

	if n := strings.TrimSpace(nickName); n != "" {
		return n
	}
	if u != 0 {
		return "用户" + strconv.FormatInt(u, 10)
	}
	return "匿名"
}

func normalizeAvatar(b64 string) string {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return ""
	}
	if strings.HasPrefix(b64, "data:") || strings.HasPrefix(b64, "http") {
		return b64
	}
	return "data:image/png;base64," + b64
}

// avatarHTML 生成一个圆形头像。有 base64 用图片，否则首字母圆形兜底。
func (v *View) avatarHTML(u int64, size int) template.HTML {
	_, avatarB64 := v.GetUserData(u)

	if src := normalizeAvatar(avatarB64); src != "" {
		return template.HTML(fmt.Sprintf(
			`<img class="gd-av" style="width:%dpx;height:%dpx" src="%s" alt="">`,
			size, size, html.EscapeString(src)))
	}
	fs := size * 42 / 100
	if fs < 9 {
		fs = 9
	}
	return template.HTML(fmt.Sprintf(
		`<span class="gd-av gd-av-fb" style="width:%dpx;height:%dpx;line-height:%dpx;font-size:%dpx;background:%s">%s</span>`,
		size, size, size, fs, avatarColor(u), html.EscapeString(firstRune(v.displayName(u)))))
}

// userChipHTML 是行内的「小头像 + 昵称」胶囊，用于话题贡献者、详情引用等。
func (v *View) userChipHTML(u int64) template.HTML {
	_, avatarB64 := v.GetUserData(u)
	var av string
	if src := normalizeAvatar(avatarB64); src != "" {
		av = fmt.Sprintf(`<img class="gd-chip-av" src="%s" alt="">`, html.EscapeString(src))
	} else {
		av = fmt.Sprintf(`<span class="gd-chip-av gd-chip-fb" style="background:%s">%s</span>`,
			avatarColor(u), html.EscapeString(firstRune(v.displayName(u))))
	}
	return template.HTML(fmt.Sprintf(
		`<span class="gd-chip">%s<span class="gd-chip-n">%s</span></span>`,
		av, html.EscapeString(v.displayName(u))))
}

func (v *View) userReasonChipHTML(u int64) template.HTML {
	var av string
	_, avatarB64 := v.GetUserData(u)
	if src := normalizeAvatar(avatarB64); src != "" {
		av = fmt.Sprintf(
			`<img class="q-user-av" src="%s" alt="">`,
			html.EscapeString(src),
		)
	} else {
		av = fmt.Sprintf(
			`<span class="q-user-av q-user-fb" style="background:%s">%s</span>`,
			avatarColor(u),
			html.EscapeString(firstRune(v.displayName(u))),
		)
	}

	return template.HTML(fmt.Sprintf(
		`<span class="q-user">%s<span class="q-user-name">%s</span></span>`,
		av,
		html.EscapeString(v.displayName(u)),
	))
}

var refRe = regexp.MustCompile(`\[(\d+)\]`)

// detailHTML 把话题详情里的 [用户ID] 替换成用户胶囊，其余文本做转义。
func (v *View) detailHTML(detail string) template.HTML {

	var b strings.Builder
	last := 0
	for _, loc := range refRe.FindAllStringSubmatchIndex(detail, -1) {
		b.WriteString(html.EscapeString(detail[last:loc[0]]))
		id, _ := strconv.ParseInt(detail[loc[2]:loc[3]], 10, 64)
		nickName, _ := v.GetUserData(id)
		if nickName != "" {
			b.WriteString(string(v.userChipHTML(id)))
		} else {
			// 找不到对应用户就保留原文（已转义）
			b.WriteString(html.EscapeString(detail[loc[0]:loc[1]]))
		}
		last = loc[1]
	}
	b.WriteString(html.EscapeString(detail[last:]))
	return template.HTML(b.String())
}

// reasonHTML 把reason里的 [用户ID] 替换成用户胶囊，其余文本做转义。
func (v *View) reasonHTML(reason string) template.HTML {

	var b strings.Builder
	last := 0
	for _, loc := range refRe.FindAllStringSubmatchIndex(reason, -1) {
		b.WriteString(html.EscapeString(reason[last:loc[0]]))
		id, _ := strconv.ParseInt(reason[loc[2]:loc[3]], 10, 64)
		nickName, _ := v.GetUserData(id)
		if nickName != "" {
			b.WriteString(string(v.userReasonChipHTML(id)))
		} else {
			// 找不到对应用户就保留原文（已转义）
			b.WriteString(html.EscapeString(reason[loc[0]:loc[1]]))
		}
		last = loc[1]
	}
	b.WriteString(html.EscapeString(reason[last:]))
	return template.HTML(b.String())
}

func maxCount(slots []HourSlot) int {
	m := 0
	for _, s := range slots {
		if s.Count > m {
			m = s.Count
		}
	}
	return m
}

func barHeight(count, max int) int {
	if max <= 0 {
		return 2
	}
	h := count * 100 / max
	if h < 2 {
		h = 2
	}
	return h
}

func pct(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s
}
