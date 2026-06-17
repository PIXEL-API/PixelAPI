package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const (
	cyberPreflightCategoryBase = "cyber_abuse"
	cyberPreflightScore        = 1
)

var cyberPreflightTargetPattern = regexp.MustCompile(`(?i)(https?://|[a-z0-9][a-z0-9-]{1,62}\.[a-z]{2,}|(?:\d{1,3}\.){3}\d{1,3})`)

type CyberPreflightResult struct {
	Flagged  bool
	Category string
	Score    float64
	Reason   string
}

func (s *ContentModerationService) CheckCyberPreflight(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.settingRepo == nil {
		return allow, nil
	}
	if !s.isRiskControlEnabled(ctx) {
		return allow, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cyber_preflight_config_load_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return allow, nil
	}
	if !cfg.CyberPreflightEnabled {
		return allow, nil
	}
	inScope, scopeCtx := s.resolveScope(ctx, cfg, input)
	if !inScope {
		return allow, nil
	}
	content := ExtractCyberPreflightInput(input.Protocol, input.Body)
	if content.IsEmpty() {
		return allow, nil
	}
	content.Normalize()
	result := EvaluateCyberPreflightTextWithRules(content.Text, cfg.CyberPreflightRules)
	if !result.Flagged {
		return allow, nil
	}

	hashText := content.Hash()
	category := cyberPreflightCategoryBase
	if strings.TrimSpace(result.Category) != "" {
		category += "/" + strings.TrimSpace(result.Category)
	}
	scores := map[string]float64{category: result.Score}
	decision := &ContentModerationDecision{
		Allowed:         false,
		Blocked:         true,
		Flagged:         true,
		Message:         defaultContentModerationCyberBlockMessage,
		StatusCode:      cyberPreflightBlockStatus(cfg),
		InputHash:       hashText,
		HighestCategory: category,
		HighestScore:    result.Score,
		CategoryScores:  scores,
		Action:          ContentModerationActionCyberBlock,
	}
	slog.Info("content_moderation.cyber_preflight_blocked",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"model", input.Model,
		"category", category,
		"reason", result.Reason,
		"input_hash", hashText)

	log := s.buildLog(input, cfg, scopeCtx, ContentModerationActionCyberBlock, true, category, result.Score, scores, content.ExcerptText(), nil, nil, "")
	if s.hashCache != nil {
		if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
			slog.Warn("content_moderation.cyber_preflight_record_hash_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
	}
	s.applyFlaggedSideEffects(ctx, cfg, log)
	if s.repo != nil {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_preflight_log_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
	}
	s.notifyRiskControlBlocked(ctx, input, decision)
	return decision, nil
}

func ExtractCyberPreflightInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ContentModerationInput{}
	}
	var parts []string
	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		addModerationText(&parts, gjson.GetBytes(body, "system").String())
		collectAllAnthropicUserMessages(gjson.GetBytes(body, "messages"), &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		collectAllOpenAIChatMessages(gjson.GetBytes(body, "messages"), &parts, &images)
	case ContentModerationProtocolOpenAIResponses:
		addModerationText(&parts, gjson.GetBytes(body, "instructions").String())
		collectAllResponsesInputs(gjson.GetBytes(body, "input"), &parts, &images)
	case ContentModerationProtocolGemini:
		collectAllGeminiContents(gjson.GetBytes(body, "contents"), &parts, &images)
	case ContentModerationProtocolOpenAIImages:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
	default:
		addModerationText(&parts, gjson.GetBytes(body, "instructions").String())
		addModerationText(&parts, gjson.GetBytes(body, "system").String())
		collectAllOpenAIChatMessages(gjson.GetBytes(body, "messages"), &parts, &images)
		collectAllResponsesInputs(gjson.GetBytes(body, "input"), &parts, &images)
		collectAllGeminiContents(gjson.GetBytes(body, "contents"), &parts, &images)
	}
	out := ContentModerationInput{
		Text:   normalizeContentModerationText(strings.Join(parts, "\n")),
		Images: normalizeModerationImages(images),
	}
	out.Normalize()
	return out
}

func EvaluateCyberPreflightText(text string) CyberPreflightResult {
	return EvaluateCyberPreflightTextWithRules(text, defaultCyberPreflightRulesConfig())
}

func EvaluateCyberPreflightTextWithRules(text string, rules ContentModerationCyberPreflightRulesConfig) CyberPreflightResult {
	rules.normalize()
	normalized := normalizeCyberPreflightText(text)
	if normalized == "" {
		return CyberPreflightResult{}
	}
	defensive := containsAnyCyberPreflightPhrase(normalized, rules.DefensiveMarkers) != ""
	standalone := containsAnyCyberPreflightPhrase(normalized, rules.StandaloneBlockMarkers)
	hardMarker := containsAnyCyberPreflightPhrase(normalized, rules.HardMarkers)
	intent := containsAnyCyberPreflightPhrase(normalized, rules.OffensiveIntentMarkers)
	credentialIntent := containsAnyCyberPreflightPhrase(normalized, rules.CredentialAbuseIntentMarkers)
	technique := containsAnyCyberPreflightPhrase(normalized, rules.TechniqueMarkers)
	credential := containsAnyCyberPreflightPhrase(normalized, rules.CredentialMarkers)
	targeted := cyberPreflightTargetPattern.MatchString(normalized) || containsAnyCyberPreflightPhrase(normalized, rules.TargetMarkers) != ""

	switch {
	case standalone != "" && !defensive:
		return cyberPreflightFlag("malware_or_intrusion", "standalone:"+standalone)
	case hardMarker != "" && intent != "":
		return cyberPreflightFlag("malware_or_intrusion", "hard_intent:"+hardMarker+"+"+intent)
	case hardMarker != "" && targeted && !defensive:
		return cyberPreflightFlag("targeted_abuse", "hard_target:"+hardMarker)
	case credential != "" && credentialIntent != "":
		return cyberPreflightFlag("credential_theft", "credential_intent:"+credential+"+"+credentialIntent)
	case technique != "" && intent != "":
		return cyberPreflightFlag("offensive_cyber", "technique_intent:"+technique+"+"+intent)
	case technique != "" && targeted && !defensive:
		return cyberPreflightFlag("targeted_abuse", "technique_target:"+technique)
	default:
		return CyberPreflightResult{}
	}
}

func defaultCyberPreflightRulesConfig() ContentModerationCyberPreflightRulesConfig {
	return ContentModerationCyberPreflightRulesConfig{
		StandaloneBlockMarkers:       cloneStrings(cyberPreflightStandaloneBlockMarkers),
		HardMarkers:                  cloneStrings(cyberPreflightHardMarkers),
		OffensiveIntentMarkers:       cloneStrings(cyberPreflightOffensiveIntentMarkers),
		CredentialAbuseIntentMarkers: cloneStrings(cyberPreflightCredentialAbuseIntentMarkers),
		TechniqueMarkers:             cloneStrings(cyberPreflightTechniqueMarkers),
		CredentialMarkers:            cloneStrings(cyberPreflightCredentialMarkers),
		TargetMarkers:                cloneStrings(cyberPreflightTargetMarkers),
		DefensiveMarkers:             cloneStrings(cyberPreflightDefensiveMarkers),
	}
}

func (rules *ContentModerationCyberPreflightRulesConfig) normalize() {
	if rules == nil {
		return
	}
	rules.StandaloneBlockMarkers = normalizeCyberPreflightRulePhrases(rules.StandaloneBlockMarkers)
	rules.HardMarkers = normalizeCyberPreflightRulePhrases(rules.HardMarkers)
	rules.OffensiveIntentMarkers = normalizeCyberPreflightRulePhrases(rules.OffensiveIntentMarkers)
	rules.CredentialAbuseIntentMarkers = normalizeCyberPreflightRulePhrases(rules.CredentialAbuseIntentMarkers)
	rules.TechniqueMarkers = normalizeCyberPreflightRulePhrases(rules.TechniqueMarkers)
	rules.CredentialMarkers = normalizeCyberPreflightRulePhrases(rules.CredentialMarkers)
	rules.TargetMarkers = normalizeCyberPreflightRulePhrases(rules.TargetMarkers)
	rules.DefensiveMarkers = normalizeCyberPreflightRulePhrases(rules.DefensiveMarkers)
}

func (rules ContentModerationCyberPreflightRulesConfig) clone() ContentModerationCyberPreflightRulesConfig {
	return ContentModerationCyberPreflightRulesConfig{
		StandaloneBlockMarkers:       cloneStrings(rules.StandaloneBlockMarkers),
		HardMarkers:                  cloneStrings(rules.HardMarkers),
		OffensiveIntentMarkers:       cloneStrings(rules.OffensiveIntentMarkers),
		CredentialAbuseIntentMarkers: cloneStrings(rules.CredentialAbuseIntentMarkers),
		TechniqueMarkers:             cloneStrings(rules.TechniqueMarkers),
		CredentialMarkers:            cloneStrings(rules.CredentialMarkers),
		TargetMarkers:                cloneStrings(rules.TargetMarkers),
		DefensiveMarkers:             cloneStrings(rules.DefensiveMarkers),
	}
}

func validateCyberPreflightRulesConfig(rules ContentModerationCyberPreflightRulesConfig) error {
	groups := map[string][]string{
		"standalone_block_markers":        rules.StandaloneBlockMarkers,
		"hard_markers":                    rules.HardMarkers,
		"offensive_intent_markers":        rules.OffensiveIntentMarkers,
		"credential_abuse_intent_markers": rules.CredentialAbuseIntentMarkers,
		"technique_markers":               rules.TechniqueMarkers,
		"credential_markers":              rules.CredentialMarkers,
		"target_markers":                  rules.TargetMarkers,
		"defensive_markers":               rules.DefensiveMarkers,
	}
	for name, values := range groups {
		if len(values) > maxCyberPreflightRulePhrases {
			return infraerrors.BadRequest("INVALID_CYBER_PREFLIGHT_RULES", fmt.Sprintf("%s 最多允许 %d 条规则", name, maxCyberPreflightRulePhrases))
		}
		for _, value := range values {
			if len([]rune(value)) > maxCyberPreflightRulePhraseRunes {
				return infraerrors.BadRequest("INVALID_CYBER_PREFLIGHT_RULES", fmt.Sprintf("%s 单条规则不能超过 %d 个字符", name, maxCyberPreflightRulePhraseRunes))
			}
		}
	}
	return nil
}

func collectAllAnthropicUserMessages(messages gjson.Result, parts *[]string, images *[]string) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(msg.Get("role").String()))
		if role == "user" || role == "system" || role == "developer" {
			collectAnthropicUserContentValue(msg.Get("content"), parts, images)
		}
		return true
	})
}

func collectAllOpenAIChatMessages(messages gjson.Result, parts *[]string, images *[]string) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(msg.Get("role").String()))
		if role == "user" || role == "system" || role == "developer" {
			collectContentValue(msg.Get("content"), parts, images)
		}
		return true
	})
}

func collectAllResponsesInputs(input gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addModerationText(parts, input.String())
	case input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			if isResponsesUserTextItem(item) || isResponsesSystemTextItem(item) {
				collectContentValue(item.Get("content"), parts, images)
				if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
					collectContentValue(item, parts, images)
				}
			}
			return true
		})
	case input.IsObject():
		if isResponsesUserTextItem(input) || isResponsesSystemTextItem(input) {
			collectContentValue(input.Get("content"), parts, images)
			if input.Get("type").String() == "input_text" || input.Get("text").Exists() {
				collectContentValue(input, parts, images)
			}
		}
	}
}

func isResponsesSystemTextItem(item gjson.Result) bool {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	return role == "system" || role == "developer"
}

func collectAllGeminiContents(contents gjson.Result, parts *[]string, images *[]string) {
	if !contents.IsArray() {
		return
	}
	contents.ForEach(func(_, content gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
		if role == "" || role == "user" || role == "system" || role == "developer" {
			if arr := content.Get("parts"); arr.IsArray() {
				arr.ForEach(func(_, part gjson.Result) bool {
					addModerationText(parts, part.Get("text").String())
					addGeminiModerationImage(images, part)
					return true
				})
			}
		}
		return true
	})
}

func cyberPreflightBlockStatus(cfg *ContentModerationConfig) int {
	if cfg == nil || cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return http.StatusForbidden
	}
	return cfg.BlockStatus
}

func cyberPreflightFlag(category string, reason string) CyberPreflightResult {
	return CyberPreflightResult{
		Flagged:  true,
		Category: category,
		Score:    cyberPreflightScore,
		Reason:   reason,
	}
}

func normalizeCyberPreflightText(text string) string {
	text = strings.ToLower(normalizeContentModerationText(text))
	replacer := strings.NewReplacer(
		"／", "/",
		"．", ".",
		"：", ":",
		"＿", "_",
		"-", " ",
		"_", " ",
		"`", " ",
		"'", " ",
		"\"", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func containsAnyCyberPreflightPhrase(text string, phrases []string) string {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return phrase
		}
	}
	return ""
}

func normalizeCyberPreflightRulePhrases(phrases []string) []string {
	out := make([]string, 0, len(phrases))
	seen := make(map[string]struct{}, len(phrases))
	for _, phrase := range phrases {
		phrase = normalizeCyberPreflightText(phrase)
		if phrase == "" {
			continue
		}
		if _, ok := seen[phrase]; ok {
			continue
		}
		seen[phrase] = struct{}{}
		out = append(out, phrase)
	}
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

var cyberPreflightHardMarkers = []string{
	"反弹 shell",
	"反弹shell",
	"reverse shell",
	"webshell",
	"web shell",
	"getshell",
	"get shell",
	"免杀",
	"绕过杀软",
	"bypass antivirus",
	"bypass av",
	"keylogger",
	"键盘记录",
	"ransomware",
	"勒索软件",
	"钓鱼页面",
	"钓鱼网站",
	"phishing kit",
	"phishing page",
	"stealer",
	"mimikatz",
	"cobalt strike",
	"meterpreter",
	"botnet",
	"僵尸网络",
	"ddos",
	"挖矿脚本",
	"cryptominer",
	"恶意软件",
	"malware",
	"后门",
}

var cyberPreflightStandaloneBlockMarkers = []string{
	"免杀",
	"绕过杀软",
	"bypass antivirus",
	"bypass av",
	"钓鱼页面",
	"钓鱼网站",
	"phishing kit",
	"phishing page",
	"keylogger",
	"键盘记录",
	"挖矿脚本",
	"cryptominer",
}

var cyberPreflightOffensiveIntentMarkers = []string{
	"帮我写",
	"写一个",
	"生成",
	"给我代码",
	"提供代码",
	"完整代码",
	"构造",
	"制作",
	"开发",
	"实现一个",
	"利用",
	"攻击",
	"入侵",
	"拿下",
	"提权",
	"绕过",
	"规避",
	"隐藏",
	"持久化",
	"横向移动",
	"盗取",
	"窃取",
	"偷取",
	"批量扫描",
	"批量爆破",
	"爆破",
	"撞库",
	"钓鱼",
	"payload",
	"exploit",
	"exploit code",
	"shellcode",
	"bypass",
	"evade",
	"attack",
	"hack",
	"compromise",
	"privilege escalation",
	"persistence",
	"lateral movement",
	"steal",
	"exfiltrate",
	"phish",
	"bruteforce",
	"brute force",
	"mass scan",
	"deploy",
	"execute",
	"dump",
}

var cyberPreflightCredentialAbuseIntentMarkers = []string{
	"盗取",
	"窃取",
	"偷取",
	"抓取",
	"导出",
	"泄露",
	"外传",
	"提取",
	"dump",
	"dumping",
	"steal",
	"exfiltrate",
	"extract",
	"leak",
	"harvest",
}

var cyberPreflightTechniqueMarkers = []string{
	"sql injection",
	"sqli",
	"xss",
	"csrf",
	"ssrf",
	"rce",
	"remote code execution",
	"命令执行",
	"代码执行",
	"漏洞利用",
	"0day",
	"zero day",
	"提权",
	"弱口令",
	"爆破",
	"撞库",
	"端口扫描",
	"批量扫描",
	"nmap",
	"masscan",
	"hydra",
	"hashcat",
	"john the ripper",
	"payload",
	"shellcode",
	"反序列化",
	"文件上传漏洞",
	"目录穿越",
	"命令注入",
	"c2",
	"command and control",
	"持久化",
	"横向移动",
	"lsass",
}

var cyberPreflightCredentialMarkers = []string{
	"密码",
	"账号密码",
	"凭证",
	"cookie",
	"cookies",
	"token",
	"access token",
	"refresh token",
	"api key",
	"apikey",
	"secret key",
	"session",
	"credential",
	"credentials",
	"password",
	"passwd",
	"ssh key",
	"私钥",
	"密钥",
}

var cyberPreflightTargetMarkers = []string{
	"真实网站",
	"目标网站",
	"公网",
	"生产环境",
	"线上环境",
	"后台",
	"登录页",
	"login",
	"admin",
	"公司内网",
	"目标服务器",
}

var cyberPreflightDefensiveMarkers = []string{
	"防御",
	"防护",
	"检测",
	"识别",
	"修复",
	"加固",
	"缓解",
	"日志",
	"审计",
	"告警",
	"监控",
	"溯源",
	"蓝队",
	"安全培训",
	"合规",
	"风险评估",
	"如何避免",
	"防止",
	"授权测试",
	"授权的",
	"自己的系统",
	"本地靶场",
	"靶场",
	"ctf",
	"capture the flag",
	"detection",
	"detect",
	"defense",
	"defensive",
	"mitigation",
	"patch",
	"hardening",
	"audit",
	"monitoring",
	"authorized test",
	"lab",
	"sandbox",
}
