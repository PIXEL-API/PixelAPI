package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAICodexAutomationHeartbeat(t *testing.T) {
	const output = `<heartbeat>
  <automation_id>flutter</automation_id>
  <current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso>
  <instructions>
Read the reference index and report changes. Preserve A &amp; B and &lt;tags&gt;.
  </instructions>
</heartbeat>`
	body := map[string]any{
		"model": "gpt-5",
		"input": []any{map[string]any{
			"type":      "function_call_output",
			"namespace": "codex_app",
			"name":      "automation_update",
			"output":    output,
		}},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	normalized, kind, changed := NormalizeOpenAICodexBootstrap(raw)
	require.True(t, changed)
	require.Equal(t, OpenAICodexBootstrapAutomation, kind)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
	require.Equal(t, output, gjson.GetBytes(normalized, "input.0.content.0.text").String())

	again, _, changedAgain := NormalizeOpenAICodexBootstrap(normalized)
	require.False(t, changedAgain)
	require.Equal(t, normalized, again)
}

func TestNormalizeOpenAICodexAutomationHeartbeatRejectsIncompleteOrUnsafeEnvelope(t *testing.T) {
	valid := `<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions>Review the project.</instructions></heartbeat>`
	for _, output := range []string{
		`<heartbeat><automation_id>wiki</automation_id></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>yesterday</current_time_iso><instructions>Review.</instructions></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions><nested/></instructions></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions> </instructions><extra>x</extra></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions> </instructions></heartbeat>`,
		`<heartbeat><automation_id>../wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions>Review.</instructions></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><automation_id>duplicate</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions>Review.</instructions></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions>Review.</instructions></heartbeat><heartbeat><automation_id>other</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions>Review.</instructions></heartbeat>`,
		`<heartbeat><automation_id>wiki</automation_id><current_time_iso>2026-09-09T00:33:34.775Z</current_time_iso><instructions source="other">Review.</instructions></heartbeat>`,
	} {
		item := map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "automation_update", "output": output}
		request := map[string]any{"model": "gpt-5", "input": []any{item}}
		raw, err := json.Marshal(request)
		require.NoError(t, err)
		got, _, changed := NormalizeOpenAICodexBootstrap(raw)
		require.False(t, changed)
		require.Equal(t, raw, got)
	}

	for _, request := range []map[string]any{
		{"model": "gpt-5", "previous_response_id": "resp-1", "input": []any{map[string]any{
			"type": "function_call_output", "namespace": "codex_app", "name": "automation_update", "output": valid,
		}}},
		{"model": "gpt-5", "input": []any{map[string]any{
			"type": "function_call_output", "namespace": "other", "name": "automation_update", "output": valid,
		}}},
		{"model": "gpt-5", "input": []any{map[string]any{
			"type": "function_call_output", "namespace": "codex_app", "name": "other", "output": valid,
		}}},
	} {
		raw, err := json.Marshal(request)
		require.NoError(t, err)
		got, _, changed := NormalizeOpenAICodexBootstrap(raw)
		require.False(t, changed)
		require.Equal(t, raw, got)
	}

	item := map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "automation_update", "output": valid, "call_id": "call-1"}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5", "input": []any{item}})
	require.NoError(t, err)
	got, _, changed := NormalizeOpenAICodexBootstrap(raw)
	require.False(t, changed)
	require.Equal(t, raw, got)
}
