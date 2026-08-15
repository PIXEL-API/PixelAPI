package service

import "strings"

// createGrokProbePayload builds the smallest Responses request accepted by
// both Grok OAuth and API-key endpoints. Keeping manual tests and background
// quota probes on one wire shape prevents a probe-only option from
// incorrectly quarantining an otherwise healthy account.
func createGrokProbePayload(model, prompt string) map[string]any {
	model = strings.TrimSpace(model)
	if model == "" {
		model = grokQuotaDefaultModel
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = grokQuotaProbeInput
	}
	return map[string]any{
		"model":  model,
		"input":  prompt,
		"stream": true,
	}
}
