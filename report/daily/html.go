package daily

import (
	"bytes"
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"html/template"
	"net/url"
	"time"
)

const reportHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    width: 390px;
    background: transparent;
    padding: 16px;
    font-family: 'AppFont', "PingFang SC", "Microsoft YaHei", sans-serif;
    -webkit-font-smoothing: antialiased;
  }

  .card {
    background: {{.Visual.BgColor}};
    border-radius: 20px;
    overflow: hidden;
    {{- if eq .Visual.BorderStyle "glow"}}
    border: 1px solid {{.Visual.AccentColor}}44;
    box-shadow: 0 0 40px {{.Visual.AccentColor}}22, 0 20px 60px rgba(0,0,0,0.5);
    {{- else if eq .Visual.BorderStyle "solid"}}
    border: 1px solid {{.Visual.AccentColor}}66;
    {{- else if eq .Visual.BorderStyle "dashed"}}
    border: 1px dashed {{.Visual.AccentColor}}55;
    {{- else if eq .Visual.BorderStyle "double"}}
    border: 3px double {{.Visual.AccentColor}}66;
    {{- else}}
    box-shadow: 0 16px 48px rgba(0,0,0,0.2);
    {{- end}}
  }

  /* ========== HEADER ========== */
  .header {
    background: {{.Visual.HeaderColor}};
    padding: 28px 24px 24px;
    position: relative;
    overflow: hidden;
  }

  /* 右上角大字水印 */
  .header-watermark {
    position: absolute;
    right: -4px;
    top: -12px;
    font-size: 100px;
    font-weight: 900;
    color: {{.Visual.AccentColor}}0d;
    letter-spacing: -6px;
    line-height: 1;
    pointer-events: none;
    font-variant-numeric: tabular-nums;
    user-select: none;
  }

  .header-top {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 20px;
    position: relative;
    z-index: 1;
  }

  .header-tag {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .tag-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: {{.Visual.AccentColor}};
    opacity: 0.7;
  }

  .tag-text {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    letter-spacing: 3px;
    opacity: 0.7;
    text-transform: uppercase;
  }

  .header-date {
    font-size: 10px;
    color: {{.Visual.TextColor}};
    opacity: 0.3;
    letter-spacing: 1.5px;
    font-variant-numeric: tabular-nums;
  }

  .title {
    font-size: 30px;
    font-weight: 800;
    color: {{.Visual.TextColor}};
    letter-spacing: -1px;
    line-height: 1.15;
    position: relative;
    z-index: 1;
    margin-bottom: 12px;
  }

  .role-line {
    display: flex;
    align-items: center;
    gap: 8px;
    position: relative;
    z-index: 1;
  }

  .role-bar {
    width: 18px;
    height: 2px;
    background: {{.Visual.AccentColor}};
    opacity: 0.5;
    border-radius: 1px;
  }

  .role-text {
    font-size: 11px;
    color: {{.Visual.TextColor}};
    opacity: 0.35;
    letter-spacing: 1.5px;
  }

  /* ========== OPENING ========== */
  .opening-wrap {
    margin: 0;
    padding: 18px 24px 20px 21px;
    background: {{.Visual.AccentColor}}0d;
    border-left: 3px solid {{.Visual.AccentColor}}99;
  }

  .opening-label {
    font-size: 9px;
    color: {{.Visual.AccentColor}};
    letter-spacing: 3px;
    opacity: 0.55;
    margin-bottom: 8px;
    text-transform: uppercase;
  }

  .opening-text {
    font-size: 14px;
    line-height: 1.9;
    color: {{.Visual.TextColor}};
    opacity: 0.65;
    font-style: italic;
  }

  /* ========== 顶部渐变分割 ========== */
  .top-divider {
    height: 1px;
    background: linear-gradient(
      90deg,
      {{.Visual.AccentColor}}aa 0%,
      {{.Visual.AccentColor}}33 50%,
      transparent 100%
    );
  }

  /* ========== SECTIONS ========== */
  .sections-wrap {
    padding: 12px 14px 8px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .section {
    border-radius: 14px;
    overflow: hidden;
    border: 1px solid {{.Visual.AccentColor}}15;
  }

  /* section头部色带 */
  .section-head {
    background: {{.Visual.AccentColor}}12;
    padding: 11px 16px;
    display: flex;
    align-items: center;
    gap: 10px;
    border-bottom: 1px solid {{.Visual.AccentColor}}10;
  }

  .section-head-bar {
    width: 3px;
    height: 14px;
    background: {{.Visual.AccentColor}};
    opacity: 0.6;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .section-title {
    font-size: 11px;
    font-weight: 700;
    color: {{.Visual.AccentColor}};
    letter-spacing: 2px;
    opacity: 0.85;
    text-transform: uppercase;
    flex: 1;
  }

  .section-body {
    padding: 14px 16px;
    background: {{.Visual.BgColor}};
  }

  .timeline-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
  
  .timeline-label {
    font-size: 11px;
    font-weight: 700;
    color: {{.Visual.AccentColor}};
    opacity: 0.85;
    letter-spacing: 2px;
    text-transform: uppercase;
  }
  
  .timeline-card {
    background: {{.Visual.AccentColor}}08;
    border-radius: 10px;
    padding: 12px 14px;
    border: 1px solid {{.Visual.AccentColor}}12;
  }
  
  .timeline-meta {
    font-size: 13px;
    font-weight: 700;
    color: {{.Visual.TextColor}};
    opacity: 0.85;
    margin-bottom: 6px;
  }
  
  .timeline-comment {
    font-size: 12.5px;
    line-height: 1.8;
    color: {{.Visual.TextColor}};
    opacity: 0.6;
  }
  
  .timeline-connector {
    display: flex;
    justify-content: center;
    padding: 2px 0;
  }
  
  .connector-line {
    width: 1px;
    height: 16px;
    background: linear-gradient(
      180deg,
      {{.Visual.AccentColor}}44,
      {{.Visual.AccentColor}}11
    );
  }



  /* ========== MVP ========== */
  .mvp-list {
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .mvp-item {
    display: flex;
    gap: 12px;
    padding: 12px 0;
    border-bottom: 1px solid {{.Visual.AccentColor}}0e;
    align-items: flex-start;
  }

  .mvp-item:first-child { padding-top: 0; }
  .mvp-item:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .mvp-rank-wrap {
    display: flex;
    flex-direction: column;
    align-items: center;
    min-width: 28px;
    padding-top: 2px;
  }

  .mvp-rank {
    font-size: 20px;
    font-weight: 900;
    color: {{.Visual.AccentColor}};
    opacity: 0.18;
    line-height: 1;
    font-variant-numeric: tabular-nums;
  }

  .mvp-item:first-child .mvp-rank { opacity: 0.55; }
  .mvp-item:nth-child(2) .mvp-rank { opacity: 0.3; }

  .mvp-right { flex: 1; min-width: 0; }

  .mvp-name-line {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
    flex-wrap: wrap;
  }

  .mvp-name {
    font-size: 15px;
    font-weight: 700;
    color: {{.Visual.TextColor}};
    opacity: 0.9;
  }

  .mvp-badge {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    background: {{.Visual.AccentColor}}18;
    padding: 2px 9px;
    border-radius: 20px;
    border: 1px solid {{.Visual.AccentColor}}28;
    letter-spacing: 0.3px;
    white-space: nowrap;
  }

  .mvp-comment {
    font-size: 13px;
    line-height: 1.85;
    color: {{.Visual.TextColor}};
    opacity: 0.58;
  }

  /* ========== INFO CARD ========== */
  .info-card {
    background: {{.Visual.AccentColor}}08;
    border-radius: 10px;
    padding: 13px 15px;
    border: 1px solid {{.Visual.AccentColor}}10;
  }

  .info-label {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    opacity: 0.6;
    margin-bottom: 7px;
    letter-spacing: 1.5px;
    font-weight: 600;
    text-transform: uppercase;
  }

  .info-text {
    font-size: 13.5px;
    line-height: 1.85;
    color: {{.Visual.TextColor}};
    opacity: 0.68;
  }

  .info-question {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px dashed {{.Visual.AccentColor}}25;
    font-size: 12.5px;
    color: {{.Visual.AccentColor}};
    opacity: 0.5;
    font-style: italic;
    line-height: 1.7;
  }

  /* ========== DIAGNOSIS ========== */
  .diagnosis-text {
    font-size: 14.5px;
    line-height: 1.9;
    color: {{.Visual.TextColor}};
    opacity: 0.75;
    font-weight: 500;
  }

  /* ========== GHOST ========== */
  .ghost-names {
    display: flex;
    flex-wrap: wrap;
    gap: 7px;
    margin-bottom: 12px;
  }

  .ghost-tag {
    font-size: 12px;
    color: {{.Visual.TextColor}};
    opacity: 0.45;
    background: {{.Visual.AccentColor}}0d;
    padding: 4px 12px;
    border-radius: 20px;
    border: 1px solid {{.Visual.AccentColor}}18;
  }

  .ghost-comment {
    font-size: 12.5px;
    color: {{.Visual.TextColor}};
    opacity: 0.4;
    font-style: italic;
    line-height: 1.8;
  }

  /* ========== FOOTER ========== */
  .footer {
    padding: 10px 20px 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .footer-left {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  .footer-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: {{.Visual.AccentColor}};
    opacity: 0.2;
  }

  .footer-text {
    font-size: 10px;
    color: {{.Visual.TextColor}};
    opacity: 0.18;
    letter-spacing: 1px;
  }

  .footer-no {
    font-size: 10px;
    color: {{.Visual.AccentColor}};
    opacity: 0.22;
    letter-spacing: 2px;
    font-variant-numeric: tabular-nums;
  }

  .info-card-mt {
      margin-top: 8px;
  }
</style>
</head>
<body>
<div class="card">

  <div class="header">
    <div class="header-watermark">{{.DateNo}}</div>
    <div class="header-top">
      <div class="header-tag">
        <div class="tag-dot"></div>
        <span class="tag-text">Daily Report</span>
      </div>
      <span class="header-date">{{.Date}}</span>
    </div>
    <div class="title">{{.Title}}</div>
    <div class="role-line">
      <div class="role-bar"></div>
      <span class="role-text">{{.GroupName}}</span>
    </div>
  </div>

  <div class="top-divider"></div>

  <div class="opening-wrap">
    <div class="opening-label">Opening</div>
    <div class="opening-text">{{.Opening}}</div>
  </div>

  <div class="top-divider"></div>

  <div class="sections-wrap">

  <!-- 首发 & 末发 -->
  <div class="section">
    <div class="section-body" style="padding-top: 14px;">
  
      <div class="timeline-card">
        <div class="timeline-header">
          <div class="section-head-bar"></div>
          <span class="timeline-label">{{.Theme.FirstHeader}}</span>
        </div>
        <div class="timeline-meta">{{.Report.FirstBlood.Nickname}}</div>
		<div class="info-label">{{.Report.FirstBlood.Time}}</div>
        <div class="timeline-comment">{{.Report.FirstBlood.Comment}}</div>
      </div>
  
      <div class="timeline-connector">
        <div class="connector-line"></div>
      </div>
  
      <div class="timeline-card">
        <div class="timeline-header">
          <div class="section-head-bar"></div>
          <span class="timeline-label">{{.Theme.EndHeader}}</span>
        </div>
        <div class="timeline-meta">{{.Report.LastWords.Nickname}}</div>
        <div class="info-label">{{.Report.LastWords.Time}}</div>
        <div class="timeline-comment">{{.Report.LastWords.Comment}}</div>
      </div>
  
    </div>
  </div>

    <div class="section">
      <div class="section-head">
        <div class="section-head-bar"></div>
        <span class="section-title">{{.Theme.MvpHeader}}</span>
      </div>
      <div class="section-body">
        <div class="mvp-list">
          {{range $i, $m := .Report.MVP}}
          <div class="mvp-item">
            <div class="mvp-rank-wrap">
              <span class="mvp-rank">{{rankStr $i}}</span>
            </div>
            <div class="mvp-right">
              <div class="mvp-name-line">
                <span class="mvp-name">{{$m.Nickname}}</span>
                <span class="mvp-badge">{{$m.Title}}</span>
              </div>
              <div class="mvp-comment">{{$m.Comment}}</div>
            </div>
          </div>
          {{end}}
        </div>
      </div>
    </div>

    <div class="section">
      <div class="section-head">
        <div class="section-head-bar"></div>
        <span class="section-title">{{.Theme.MomentHeader}}</span>
      </div>
      <div class="section-body">
        <div class="info-card">
          <div class="info-label">{{.Report.Moment.Time}}</div>
          <div class="info-text">{{.Report.Moment.Comment}}</div>
        </div>
      </div>
    </div>

    <div class="section">
      <div class="section-head">
        <div class="section-head-bar"></div>
        <span class="section-title">{{.Theme.InteractionHeader}}</span>
      </div>
      <div class="section-body">
        <div class="info-card">
          <div class="info-label">{{.Report.Interaction.Type}}</div>
          <div class="info-text">{{.Report.Interaction.Comment}}</div>
        </div>
      </div>
    </div>

	<!-- 冷知识 -->
	<div class="section">
	  <div class="section-head">
		<div class="section-head-bar"></div>
		<span class="section-title">{{.Theme.TriviaHeader}}</span>
	  </div>
	  <div class="section-body">
		{{range $i, $t := .Report.Trivia}}
		<div class="info-card{{if gt $i 0}} info-card-mt{{end}}">
		  <div class="info-text">{{$t.Fact}}</div>
		  {{if $t.Question}}
		  <div class="info-question">{{$t.Question}}</div>
		  {{end}}
		</div>
		{{end}}
	  </div>
	</div>

    <div class="section">
      <div class="section-head">
        <div class="section-head-bar"></div>
        <span class="section-title">{{.Theme.DiagnosisHeader}}</span>
      </div>
      <div class="section-body">
        <div class="diagnosis-text">{{.Report.Diagnosis}}</div>
      </div>
    </div>

    {{if .Report.Ghosts.Names}}
    <div class="section">
      <div class="section-head">
        <div class="section-head-bar"></div>
        <span class="section-title">{{.Theme.GhostHeader}}</span>
      </div>
      <div class="section-body">
        <div class="ghost-names">
          {{range .Report.Ghosts.Names}}
          <span class="ghost-tag">{{.}}</span>
          {{end}}
        </div>
        {{if .Report.Ghosts.Comment}}
        <div class="ghost-comment">{{.Report.Ghosts.Comment}}</div>
        {{end}}
      </div>
    </div>
    {{end}}

  </div>

  <div class="footer">
    <div class="footer-left">
      <div class="footer-dot"></div>
      <span class="footer-text">Generated by {{.BotNickName}}</span>
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
	GroupName   string
	Opening     string
	BotNickName string
	Theme       *DailyTheme
	Report      ReportJSON
	Visual      ThemeVisual
}

func newReportTemplateData(t time.Time, theme *DailyTheme, report ReportJSON, groupName string, botNickName string) reportTemplateData {
	return reportTemplateData{
		Date:        t.Format("2006-01-02"),
		DateNo:      t.Format("0102"),
		Title:       report.Title,
		GroupName:   groupName,
		Theme:       theme,
		Opening:     report.Opening,
		Report:      report,
		Visual:      theme.Visual,
		BotNickName: botNickName,
	}
}

// rankStr 把0-based index转成排名符号
func (r *reportTemplateData) rankStr(i int) string {
	return []string{"01", "02", "03", "04", "05"}[i]
}

func (r *reportTemplateData) renderReportImage(chromeAddr string, group int64) ([]byte, error) {
	var err error

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

	htmlData := url.PathEscape(buf.String())

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

	navigate := "data:text/html;charset=utf-8," + htmlData

	//logrus.Infof("navigate: %s", navigate)

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
