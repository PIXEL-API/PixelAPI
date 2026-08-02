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

var (
	cyberPreflightTargetPattern = regexp.MustCompile(`(?i)(https?://|[a-z0-9][a-z0-9-]{1,62}\.[a-z]{2,}|(?:\d{1,3}\.){3}\d{1,3})`)
	cyberPreflightTextReplacer  = strings.NewReplacer(
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
)

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
	// 未配置任何规则时直接放行，避免为一次必然不命中的判定去遍历整个请求体。
	if cfg.CyberPreflightRules.IsEmpty() {
		return allow, nil
	}
	inScope, scopeCtx := s.resolveScope(ctx, cfg, input)
	if !inScope {
		return allow, nil
	}
	var content ContentModerationInput
	if input.ContentSource != nil {
		content = input.ContentSource.CyberPreflightInputCopy()
	} else {
		content = ExtractCyberPreflightInput(input.Protocol, input.Body)
	}
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
	s.recordDynamicSamplingRiskEvent(ctx, cfg, input, "cyber_preflight")
	s.applyFlaggedSideEffects(ctx, cfg, log)
	if s.repo != nil {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_preflight_log_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
	}
	s.notifyRiskControlBlocked(ctx, input, decision)
	return decision, nil
}

func extractOpenAIResponsesCyberPreflightInput(instructions, input gjson.Result) ContentModerationInput {
	collector := newModerationInputCollector()
	if instructions.Exists() {
		collector.AddText(instructions.String())
	}
	collectAllResponsesInputsBounded(input, collector)
	return collector.Input()
}

func ExtractCyberPreflightInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ContentModerationInput{}
	}
	if protocol == ContentModerationProtocolOpenAIResponses {
		return extractOpenAIResponsesCyberPreflightInput(
			gjson.GetBytes(body, "instructions"),
			gjson.GetBytes(body, "input"),
		)
	}
	var parts []string
	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		addModerationText(&parts, gjson.GetBytes(body, "system").String())
		collectAllAnthropicUserMessages(gjson.GetBytes(body, "messages"), &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		collectAllOpenAIChatMessages(gjson.GetBytes(body, "messages"), &parts, &images)
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

func EvaluateCyberPreflightTextWithRules(text string, rules ContentModerationCyberPreflightRulesConfig) CyberPreflightResult {
	if rules.IsEmpty() {
		return CyberPreflightResult{}
	}
	normalized := normalizeCyberPreflightText(text)
	if normalized == "" {
		return CyberPreflightResult{}
	}
	matches := loadCyberPreflightRuleMatcher(rules).Match(normalized)
	defensive := matches.Defensive != ""
	standalone := matches.StandaloneBlock
	hardMarker := matches.Hard
	intent := matches.OffensiveIntent
	credentialIntent := matches.CredentialAbuseIntent
	technique := matches.Technique
	credential := matches.Credential
	targeted := cyberPreflightTargetPattern.MatchString(normalized) || matches.Target != ""

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

// defaultCyberPreflightRulesConfig 返回空词表：本地预检默认不内置任何拦截词。
// 内置词表对以编码为主的流量误杀过高（凭证/技术类规则无防御豁免、目标正则会匹配任意文件名），
// 因此规则完全交由管理员在后台按自身流量配置；未配置时预检不生效。
func defaultCyberPreflightRulesConfig() ContentModerationCyberPreflightRulesConfig {
	return ContentModerationCyberPreflightRulesConfig{
		StandaloneBlockMarkers:       []string{},
		HardMarkers:                  []string{},
		OffensiveIntentMarkers:       []string{},
		CredentialAbuseIntentMarkers: []string{},
		TechniqueMarkers:             []string{},
		CredentialMarkers:            []string{},
		TargetMarkers:                []string{},
		DefensiveMarkers:             []string{},
	}
}

// IsEmpty 表示未配置任何本地预检规则，此时预检不做任何判定。
func (rules ContentModerationCyberPreflightRulesConfig) IsEmpty() bool {
	return len(rules.StandaloneBlockMarkers) == 0 &&
		len(rules.HardMarkers) == 0 &&
		len(rules.OffensiveIntentMarkers) == 0 &&
		len(rules.CredentialAbuseIntentMarkers) == 0 &&
		len(rules.TechniqueMarkers) == 0 &&
		len(rules.CredentialMarkers) == 0 &&
		len(rules.TargetMarkers) == 0 &&
		len(rules.DefensiveMarkers) == 0
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

func collectAllResponsesInputsBounded(input gjson.Result, collector *moderationInputCollector) {
	if collector == nil {
		return
	}
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		if collector.runeCount < maxModerationInputRunes {
			collector.AddText(input.String())
		}
	case input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			if isResponsesUserTextItem(item) || isResponsesSystemTextItem(item) {
				collectContentValueBounded(item.Get("content"), collector)
				if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
					collectContentValueBounded(item, collector)
				}
			}
			return true
		})
	case input.IsObject():
		if isResponsesUserTextItem(input) || isResponsesSystemTextItem(input) {
			collectContentValueBounded(input.Get("content"), collector)
			if input.Get("type").String() == "input_text" || input.Get("text").Exists() {
				collectContentValueBounded(input, collector)
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
	return strings.Join(strings.Fields(cyberPreflightTextReplacer.Replace(text)), " ")
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
