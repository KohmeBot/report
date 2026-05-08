package daily

import (
	"fmt"
	"strings"
)

const System = `
你是一个毒舌但真诚的群聊观察员。你的任务是生成每日群聊日报。
你的风格特点：
- 说话像脱口秀主持人，有时讽刺但不恶意
- 善用对比和夸张，把平平无奇的聊天说成史诗级事件
- 爱给人贴"称号"，称号要根据他今天的发言内容定制，不能是通用称号
- 对潜水的人冷嘲热讽，对话痨的人佩服又嫌弃
- 数据要具体，不说"发言很多"，说"发了89条，平均每16分钟说一句"
- 戳一戳次数、@次数、短句率这些数据在你眼里都是社交行为的线索
- 输出格式是群聊消息，不要用markdown，用emoji代替格式符号
- 引用当事人原话时加「」，原话是证据，不能改
禁止：
- 不要输出任何涉及政治、色情、人身攻击的内容
- 称号不能是"话痨""潜水王"这种通用的，要根据内容定制
`

const UserPrompt = `
根据以下今日群聊数据，出具一份日报，直接发群里，不要有任何开场白和结束语。
%s
按以下格式输出，每段之间空一行：
🔬 今日MVP鉴定
选今天存在感最强的1人。格式："昵称 · 专属称号"，称号必须从他今天的具体行为提炼（复读内容、孤独指数、连发次数、情绪特征都可以作为称号来源）。然后1-2句话说清楚凭什么，必须引用他的原话或具体数据作为证据。
🩻 今日社交体检报告
从以下异常指标里选最突出的1-2个，用体检报告的口吻写诊断结论：
- 孤独指数异常（大量发言无人回应）
- 复读症（同一句话说了N次）
- 情绪异常（感叹号/问号爆表）
- 词穷症（发言多但词汇量极低）
- 连发综合征（多次60秒内爆发）
- 社交寄生（被回复多但自己从不主动回复别人）
格式："患者：昵称，症状：……，病因推测：……，建议：……"
🌡️ 今日群体诊断
根据高频词和整体数据，用一句话诊断今天全群的精神状态。
格式："根据今日[具体数据]，本群集体症状为：[荒诞但合理的结论]"
数据要真实，结论要荒诞。
🎭 人物速写
只写今日发言前3名，每人一行：
「昵称」[时间段]出没，[用2个具体数据描述行为]，代表作：[引用原话]。综合评价：[两个字]。
👻 今日幽灵报告
今日发言2条以下的人，一句话带过。
格式："经侦查，以下成员今日行踪成谜：[名单]。根据现有证据推测，他们今天在[一个荒诞但合理的推测]。"
字数450字以内，让人看完想截图。
`

// BuildPrompt 把DailyReport拼成喂给AI的结构化文本
func BuildPrompt(r *DailyReport) string {
	var sb strings.Builder

	// 基本信息
	sb.WriteString(fmt.Sprintf("=== %s 群聊日报数据 ===\n\n", r.Date))
	sb.WriteString(fmt.Sprintf("【基本数据】\n今日发言人数：%d人\n今日总消息数：%d条\n\n",
		r.ActiveUsers, r.TotalMsg))

	// 活跃时段
	sb.WriteString("【活跃时段Top5】\n")
	for i, h := range r.HourStats {
		sb.WriteString(fmt.Sprintf("  %d. %02d点 —— %d条\n", i+1, h.Hour, h.Count))
	}

	// 找出最冷清时段（0条发言的小时）
	silentHours := findSilentHours(r.HourStats)
	if len(silentHours) > 0 {
		sb.WriteString(fmt.Sprintf("群沉默时段：%s（大家都不在）\n", formatHours(silentHours)))
	}
	sb.WriteString("\n")

	// 群友排行（只取前10，太多token爆炸）
	sb.WriteString("【群友今日表现】\n")
	limit := len(r.UserStats)
	if limit > 10 {
		limit = 10
	}
	for i, stat := range r.UserStats[:limit] {
		sb.WriteString(buildUserBlock(i+1, stat))
	}

	// 潜水（发言<=2条的人）
	ghosts := []string{}
	for _, stat := range r.UserStats {
		if stat.MsgCount <= 2 {
			ghosts = append(ghosts, fmt.Sprintf("%s(%d条)", stat.Nickname, stat.MsgCount))
		}
	}
	if len(ghosts) > 0 {
		sb.WriteString(fmt.Sprintf("\n【今日边缘人】\n%s\n", strings.Join(ghosts, " / ")))
	}

	// 关键词
	if len(r.TopKeywords) > 0 {
		sb.WriteString("\n【今日高频词】\n")
		parts := make([]string, 0, len(r.TopKeywords))
		for _, kw := range r.TopKeywords {
			parts = append(parts, fmt.Sprintf("%s(%d次)", kw.Word, kw.Count))
		}
		sb.WriteString(strings.Join(parts, " / ") + "\n")
	}

	return sb.String()
}

func buildUserBlock(rank int, stat UserStat) string {
	var sb strings.Builder

	// 基础行
	sb.WriteString(fmt.Sprintf("%d. %s —— 发言%d条\n", rank, stat.Nickname, stat.MsgCount))

	traits := []string{}

	// 短句率
	if stat.MsgCount > 0 {
		shortRate := stat.ShortCount * 100 / stat.MsgCount
		switch {
		case shortRate >= 80:
			traits = append(traits, fmt.Sprintf("发言%d%%是短句，惜字如金", shortRate))
		case shortRate >= 50:
			traits = append(traits, "说话偏短，能一个字绝不两个字")
		}
	}

	// 图片/表情包
	if stat.MsgCount > 0 {
		imgRate := stat.ImageCount * 100 / stat.MsgCount
		switch {
		case imgRate >= 60:
			traits = append(traits, fmt.Sprintf("发了%d张图占发言60%%+，靠图说话", stat.ImageCount))
		case stat.ImageCount >= 5:
			traits = append(traits, fmt.Sprintf("发了%d张图", stat.ImageCount))
		}
	}

	// 戳一戳
	switch {
	case stat.PokeCount >= 10:
		traits = append(traits, fmt.Sprintf("戳了别人%d次，戳戳狂魔", stat.PokeCount))
	case stat.PokeCount >= 3:
		traits = append(traits, fmt.Sprintf("戳了别人%d次", stat.PokeCount))
	case stat.PokeCount == 1:
		traits = append(traits, "戳了别人1次，不知道什么心态")
	}

	// at别人
	switch {
	case stat.AtCount >= 10:
		traits = append(traits, fmt.Sprintf("@了别人%d次，群里的点名机器", stat.AtCount))
	case stat.AtCount >= 3:
		traits = append(traits, fmt.Sprintf("@了别人%d次", stat.AtCount))
	}

	// 回复行为
	switch {
	case stat.ReplyCount >= 10:
		traits = append(traits, fmt.Sprintf("引用回复了%d次，积极参与型或连环杠精", stat.ReplyCount))
	case stat.ReplyCount >= 3:
		traits = append(traits, fmt.Sprintf("引用回复了%d次", stat.ReplyCount))
	}

	// 被回复
	switch {
	case stat.BeReplied >= 5:
		traits = append(traits, fmt.Sprintf("被别人引用回复或@%d次，发言有人接", stat.BeReplied))
	case stat.BeReplied == 0 && stat.MsgCount >= 30:
		traits = append(traits, "发了这么多条没人引用回复")
	}

	// 孤独指数
	if stat.MsgCount > 0 {
		lonelyRate := stat.LonelyCount * 100 / stat.MsgCount
		switch {
		case lonelyRate >= 80:
			traits = append(traits, fmt.Sprintf(
				"%d条发言里%d%%发出后5分钟无人回应，今日最孤独", stat.MsgCount, lonelyRate))
		case lonelyRate >= 50:
			traits = append(traits, fmt.Sprintf("超过一半发言没人接，有点冷场"))
		}
	}

	// 连发
	switch {
	case stat.BurstCount >= 5:
		traits = append(traits, fmt.Sprintf("连发爆发%d次，间歇性话痨确诊", stat.BurstCount))
	case stat.BurstCount >= 2:
		traits = append(traits, fmt.Sprintf("有%d次60秒内连发3条以上", stat.BurstCount))
	}

	// 情绪
	if stat.ExclamCount >= 10 {
		traits = append(traits, fmt.Sprintf("用了%d个感叹号，今天情绪高涨", stat.ExclamCount))
	}
	if stat.QuestionCount >= 8 {
		traits = append(traits, fmt.Sprintf("问了%d个问号，今天十万个为什么", stat.QuestionCount))
	}
	if stat.EllipsisCount >= 5 {
		traits = append(traits, fmt.Sprintf("用了%d个省略号，意味深长还是说不完整", stat.EllipsisCount))
	}

	// 词汇量 vs 发言量的矛盾
	if stat.MsgCount >= 10 && stat.VocabSize > 0 {
		switch {
		case stat.VocabSize < 20 && stat.MsgCount >= 15:
			traits = append(traits, fmt.Sprintf(
				"发了%d条但只用了%d种字，翻来覆去就那几个词", stat.MsgCount, stat.VocabSize))
		case stat.VocabSize >= 80:
			traits = append(traits, fmt.Sprintf("用词丰富，今天输出了%d种不同的字", stat.VocabSize))
		}
	}

	// 平均发言长度
	switch {
	case stat.AvgMsgLen >= 30:
		traits = append(traits, fmt.Sprintf("平均每条%d字，长篇大论型选手", stat.AvgMsgLen))
	case stat.AvgMsgLen <= 3 && stat.MsgCount >= 10:
		traits = append(traits, fmt.Sprintf("平均每条只有%d字，极简主义者", stat.AvgMsgLen))
	}

	// 复读
	if stat.RepeatCount >= 3 {
		preview := stat.RepeatMsg
		if runeLen(preview) > 15 {
			preview = string([]rune(preview)[:15]) + "..."
		}
		traits = append(traits, fmt.Sprintf(
			"今天把「%s」说了%d次，人工复读机", preview, stat.RepeatCount))
	}

	// 时间特征
	activeHours := stat.LastHour - stat.FirstHour
	switch {
	case stat.NightOwl && stat.FirstHour >= 22:
		traits = append(traits, fmt.Sprintf(
			"%02d点开始发言%02d点还没睡，全程夜班", stat.FirstHour, stat.LastHour))
	case stat.NightOwl:
		traits = append(traits, fmt.Sprintf(
			"凌晨还在发言，%02d点才消失", stat.LastHour))
	case stat.FirstHour <= 7 && stat.LastHour <= 12:
		traits = append(traits, fmt.Sprintf(
			"%02d点就开始发言，早鸟型，%02d点沉默", stat.FirstHour, stat.LastHour))
	case activeHours >= 14:
		traits = append(traits, fmt.Sprintf(
			"从%02d点聊到%02d点，全天候在线", stat.FirstHour, stat.LastHour))
	case activeHours <= 1 && stat.MsgCount >= 10:
		traits = append(traits, fmt.Sprintf(
			"1小时内集中发了%d条，打完就跑", stat.MsgCount))
	}

	// 写入特征
	for _, t := range traits {
		sb.WriteString(fmt.Sprintf("   · %s\n", t))
	}

	// 代表发言
	if len(stat.SampleMsgs) > 0 {
		quoted := make([]string, 0, len(stat.SampleMsgs))
		for _, msg := range stat.SampleMsgs {
			r := []rune(msg)
			if len(r) > 40 {
				msg = string(r[:40]) + "..."
			}
			quoted = append(quoted, "「"+msg+"」")
		}
		sb.WriteString("   代表发言：" + strings.Join(quoted, " / ") + "\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

func findSilentHours(activeHours []HourStat) []int {
	active := make(map[int]bool)
	for _, h := range activeHours {
		active[h.Hour] = true
	}
	silent := []int{}
	for h := 0; h < 24; h++ {
		if !active[h] {
			silent = append(silent, h)
		}
	}
	return silent
}

func formatHours(hours []int) string {
	parts := make([]string, len(hours))
	for i, h := range hours {
		parts[i] = fmt.Sprintf("%02d点", h)
	}
	if len(parts) > 5 {
		parts = parts[:5]
		return strings.Join(parts, "、") + "等"
	}
	return strings.Join(parts, "、")
}
