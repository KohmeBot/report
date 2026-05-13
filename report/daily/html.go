package daily

import (
	"bytes"
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
	"html/template"
	"os"
	"path/filepath"
	"time"
)

const reportHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    width: 480px;
    background: transparent;
    padding: 20px;
    font-family: "PingFang SC", "Microsoft YaHei", sans-serif;
  }

  .card {
    background: {{.Visual.BgColor}};
    border-radius: 4px;
    overflow: hidden;
    {{- if eq .Visual.BorderStyle "glow"}}
    border: 1px solid {{.Visual.AccentColor}}44;
    box-shadow: 0 0 40px {{.Visual.AccentColor}}22, 0 20px 60px rgba(0,0,0,0.5);
    {{- else if eq .Visual.BorderStyle "solid"}}
    border: 1px solid {{.Visual.AccentColor}}88;
    {{- else}}
    box-shadow: 0 20px 60px rgba(0,0,0,0.25);
    {{- end}}
  }

  /* 顶部 header */
  .header {
    background: {{.Visual.HeaderColor}};
    padding: 28px 32px 24px;
    position: relative;
    overflow: hidden;
  }

  .header-watermark {
    position: absolute;
    right: -8px;
    top: -4px;
    font-size: 88px;
    font-weight: 900;
    color: {{.Visual.AccentColor}}0f;
    letter-spacing: -4px;
    line-height: 1;
    user-select: none;
    font-variant-numeric: tabular-nums;
  }

  .header-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 14px;
  }

  .meta-line {
    width: 20px;
    height: 1px;
    background: {{.Visual.AccentColor}};
    opacity: 0.6;
  }

  .meta-label {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    letter-spacing: 3px;
    opacity: 0.8;
    text-transform: uppercase;
  }

  .meta-date {
    font-size: 10px;
    color: {{.Visual.TextColor}};
    opacity: 0.35;
    letter-spacing: 2px;
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  .title {
    font-size: 26px;
    font-weight: 700;
    color: {{.Visual.TextColor}};
    letter-spacing: -0.5px;
    line-height: 1.25;
    position: relative;
    z-index: 1;
  }

  .role-line {
    margin-top: 8px;
    display: flex;
    align-items: center;
    gap: 8px;
    position: relative;
    z-index: 1;
  }

  .role-dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: {{.Visual.AccentColor}};
    opacity: 0.7;
    flex-shrink: 0;
  }

  .role-text {
    font-size: 11px;
    color: {{.Visual.TextColor}};
    opacity: 0.4;
    letter-spacing: 1.5px;
  }

  .accent-divider {
    height: 1px;
    background: linear-gradient(
      90deg,
      {{.Visual.AccentColor}}cc 0%,
      {{.Visual.AccentColor}}44 60%,
      transparent 100%
    );
  }

  .accent-divider {
    height: 1px;
    background: linear-gradient(90deg, {{.Visual.AccentColor}}cc, {{.Visual.AccentColor}}44, transparent);
  }

  .opening-wrap {
    padding: 16px 32px;
    background: {{.Visual.AccentColor}}0c;
    border-left: 2px solid {{.Visual.AccentColor}}88;
  }
  .opening-label {
    font-size: 9px; color: {{.Visual.AccentColor}};
    letter-spacing: 2.5px; opacity: 0.7;
    margin-bottom: 6px; text-transform: uppercase;
  }
  .opening-text {
    font-size: 13px; line-height: 1.8;
    color: {{.Visual.TextColor}}; opacity: 0.75;
    font-style: italic;
  }

  /* 通用板块 */
  .section {
    padding: 16px 32px;
    border-bottom: 1px solid {{.Visual.AccentColor}}11;
  }
  .section:last-of-type { border-bottom: none; }

  .section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
  }
  .section-line {
    width: 14px; height: 1.5px;
    background: {{.Visual.AccentColor}}; opacity: 0.7;
  }
  .section-title {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    letter-spacing: 2px;
    opacity: 0.8;
    text-transform: uppercase;
  }

  /* MVP列表 */
  .mvp-item {
    display: flex;
    gap: 12px;
    margin-bottom: 12px;
    align-items: flex-start;
  }
  .mvp-item:last-child { margin-bottom: 0; }

  .mvp-rank {
    font-size: 11px;
    color: {{.Visual.AccentColor}};
    opacity: 0.5;
    font-weight: 700;
    min-width: 18px;
    padding-top: 1px;
    font-variant-numeric: tabular-nums;
  }
  .mvp-right { flex: 1; }
  .mvp-name-line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 4px;
  }
  .mvp-name {
    font-size: 13.5px;
    font-weight: 600;
    color: {{.Visual.TextColor}};
    opacity: 0.9;
  }
  .mvp-title {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    opacity: 0.7;
    background: {{.Visual.AccentColor}}18;
    padding: 1px 7px;
    border-radius: 2px;
    letter-spacing: 0.5px;
  }
  .mvp-comment {
    font-size: 12.5px;
    line-height: 1.75;
    color: {{.Visual.TextColor}};
    opacity: 0.65;
  }

  /* 单行信息块（moment/interaction/trivia/diagnosis） */
  .info-block {
    background: {{.Visual.AccentColor}}08;
    border-radius: 3px;
    padding: 10px 14px;
  }
  .info-time {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    opacity: 0.7;
    margin-bottom: 5px;
    letter-spacing: 1px;
  }
  .info-text {
    font-size: 13px;
    line-height: 1.75;
    color: {{.Visual.TextColor}};
    opacity: 0.75;
  }
  .info-question {
    margin-top: 6px;
    font-size: 12px;
    color: {{.Visual.AccentColor}};
    opacity: 0.6;
    font-style: italic;
  }

  /* 幽灵 */
  .ghost-names {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 8px;
  }
  .ghost-tag {
    font-size: 11px;
    color: {{.Visual.TextColor}};
    opacity: 0.45;
    background: {{.Visual.AccentColor}}10;
    padding: 2px 8px;
    border-radius: 2px;
    border: 1px solid {{.Visual.AccentColor}}22;
  }
  .ghost-comment {
    font-size: 12px;
    color: {{.Visual.TextColor}};
    opacity: 0.5;
    font-style: italic;
    line-height: 1.7;
  }

  /* 诊断单独样式 */
  .diagnosis-text {
    font-size: 14px;
    line-height: 1.8;
    color: {{.Visual.TextColor}};
    opacity: 0.8;
    font-weight: 500;
  }

  .footer {
    padding: 12px 32px 18px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .footer-left { display: flex; align-items: center; gap: 6px; }
  .footer-accent { width: 16px; height: 1px; background: {{.Visual.AccentColor}}; opacity: 0.4; }
  .footer-text { font-size: 10px; color: {{.Visual.TextColor}}; opacity: 0.25; letter-spacing: 1.5px; }
  .footer-no { font-size: 10px; color: {{.Visual.AccentColor}}; opacity: 0.3; letter-spacing: 2px; }
</style>
</head>
<body>
<div class="card">
  <!-- header 同之前 -->
  <div class="header">
    <div class="header-watermark">{{.DateNo}}</div>
    <div class="header-meta">
      <div class="meta-line"></div>
      <span class="meta-label">Daily Report</span>
      <span class="meta-date">{{.Date}}</span>
    </div>
    <div class="title">{{.Title}}</div>
    <div class="role-line">
      <div class="role-dot"></div>
      <span class="role-text">{{.Role}}</span>
    </div>
  </div>

  <div class="accent-divider"></div>

  <!-- 开场白 -->
  <div class="opening-wrap">
    <div class="opening-label">Opening</div>
    <div class="opening-text">{{.Opening}}</div>
  </div>

  <div class="accent-divider"></div>

  <!-- MVP -->
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.MvpHeader}}</span>
    </div>
    {{range $i, $m := .Report.MVP}}
    <div class="mvp-item">
      <span class="mvp-rank">{{rankStr $i}}</span>
      <div class="mvp-right">
        <div class="mvp-name-line">
          <span class="mvp-name">{{$m.Nickname}}</span>
          <span class="mvp-title">{{$m.Title}}</span>
        </div>
        <div class="mvp-comment">{{$m.Comment}}</div>
      </div>
    </div>
    {{end}}
  </div>

  <!-- 关键时刻 -->
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.MomentHeader}}</span>
    </div>
    <div class="info-block">
      <div class="info-time">{{.Report.Moment.Time}}</div>
      <div class="info-text">{{.Report.Moment.Comment}}</div>
    </div>
  </div>

  <!-- 社交图谱 -->
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.InteractionHeader}}</span>
    </div>
    <div class="info-block">
      <div class="info-time">{{.Report.Interaction.From}} → {{.Report.Interaction.To}}</div>
      <div class="info-text">{{.Report.Interaction.Comment}}</div>
    </div>
  </div>

  <!-- 冷知识 -->
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.TriviaHeader}}</span>
    </div>
    <div class="info-block">
      <div class="info-text">{{.Report.Trivia.Fact}}</div>
      <div class="info-question">{{.Report.Trivia.Question}}</div>
    </div>
  </div>

  <!-- 群体诊断 -->
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.DiagnosisHeader}}</span>
    </div>
    <div class="diagnosis-text">{{.Report.Diagnosis}}</div>
  </div>

  <!-- 失踪人口 -->
  {{if .Report.Ghosts.Names}}
  <div class="section">
    <div class="section-header">
      <div class="section-line"></div>
      <span class="section-title">{{.Theme.GhostHeader}}</span>
    </div>
    <div class="ghost-names">
      {{range .Report.Ghosts.Names}}
      <span class="ghost-tag">{{.}}</span>
      {{end}}
    </div>
    <div class="ghost-comment">{{.Report.Ghosts.Comment}}</div>
  </div>
  {{end}}

  <div class="footer">
    <div class="footer-left">
      <div class="footer-accent"></div>
      <span class="footer-text">GenerateBy {{.BotNickName}}</span>
    </div>
    <span class="footer-no">NO.{{.DateNo}}</span>
  </div>
</div>
</body>
</html>`

type reportTemplateData struct {
	Date        string
	DateNo      string
	Title       string
	Role        string
	Opening     string
	BotNickName string
	Theme       *DailyTheme
	Report      ReportJSON
	Visual      ThemeVisual
}

func newReportTemplateData(t time.Time, theme *DailyTheme, report ReportJSON, BotNickName string) reportTemplateData {
	return reportTemplateData{
		Date:        t.Format("2006-01-02"),
		DateNo:      t.Format("0102"),
		Title:       report.Title,
		Role:        theme.Role,
		Theme:       theme,
		Opening:     theme.Opening,
		Report:      report,
		Visual:      theme.Visual,
		BotNickName: BotNickName,
	}
}

// rankStr 把0-based index转成排名符号
func (r *reportTemplateData) rankStr(i int) string {
	return []string{"01", "02", "03", "04", "05"}[i]
}

func (r *reportTemplateData) renderReportImage(chromeAddr string, group int64, path string) ([]byte, error) {
	var err error
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		return nil, err
	}
	funcMap := template.FuncMap{
		"rankStr": r.rankStr,
	}

	// 1. 渲染HTML
	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportHTML)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, r)
	if err != nil {
		return nil, err
	}

	// 写成临时文件，chromedp加载本地文件最稳定
	tmpFile, err := os.CreateTemp(path, "report-*.html")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(buf.Bytes())
	tmpFile.Close()

	// 一分钟超时
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	if chromeAddr != "" {
		ctx, cancel = chromedp.NewRemoteAllocator(ctx, chromeAddr)
		defer cancel()
	}

	// 2. chromedp截图
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	navigate := "file://" + tmpFile.Name()

	logrus.Infof("navigate: %s", navigate)

	var imgBuf []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(
			440,
			1000,
			chromedp.EmulateScale(3),
		),
		chromedp.Navigate(navigate),
		// 等待内容渲染完成
		chromedp.WaitReady(".card"),
		// 截取card元素，不是整个页面
		chromedp.Screenshot(".card", &imgBuf, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}

	return imgBuf, nil
}
