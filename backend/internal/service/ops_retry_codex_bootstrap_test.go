package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const opsRetryAutomationBootstrap = "Automation: Scheduled review\n" +
	"Automation ID: scheduled-review\n" +
	"Automation memory: $CODEX_HOME/automations/scheduled-review/memory.md\n" +
	"Last run: never\n\nReview the project."

const opsRetryDelegationBootstrap = "<codex_delegation><source_thread_id>thread-1</source_thread_id><input>Continue the investigation.</input></codex_delegation>"

func opsRetryBootstrapBody(namespace, name, output string) string {
	return `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"` + namespace + `","name":"` + name + `","output":` + string(mustJSONBytes(output)) + `}]}`
}

func mustJSONBytes(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestPrepareOpsRetryBodyNormalizesTopLevelResponsesBootstrap(t *testing.T) {
	tests := []struct {
		name string
		mode string
		body string
		kind OpenAICodexBootstrapKind
	}{
		{name: "client automation", mode: OpsRetryModeClient, body: opsRetryBootstrapBody("codex_app", "automation_update", opsRetryAutomationBootstrap), kind: OpenAICodexBootstrapAutomation},
		{name: "client delegation", mode: OpsRetryModeClient, body: opsRetryBootstrapBody("codex_tui", "send_message_to_thread", opsRetryDelegationBootstrap), kind: OpenAICodexBootstrapDelegation},
		{name: "pinned automation", mode: OpsRetryModeUpstream, body: opsRetryBootstrapBody("codex_app", "automation_update", opsRetryAutomationBootstrap), kind: OpenAICodexBootstrapAutomation},
		{name: "pinned delegation", mode: OpsRetryModeUpstream, body: opsRetryBootstrapBody("codex_app", "create_thread", opsRetryDelegationBootstrap), kind: OpenAICodexBootstrapDelegation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/openai/v1/responses"}, RequestBody: test.body}
			got := prepareOpsRetryBody(test.mode, log)

			require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
			require.Equal(t, "user", gjson.GetBytes(got, "input.0.role").String())
			analysis, err := AnalyzeOpenAIResponsesRequest(got)
			require.NoError(t, err)
			require.False(t, analysis.FunctionCallOutputValidation.HasFunctionCallOutputMissingCallID)
			_, kind, changed := NormalizeOpenAICodexBootstrap([]byte(test.body))
			require.True(t, changed)
			require.Equal(t, test.kind, kind)
			require.Equal(t, test.body, log.RequestBody, "stored evidence must remain unchanged")
		})
	}
}

func TestPrepareOpsRetryBodyPreservesNonExecutableEvidence(t *testing.T) {
	validBootstrap := opsRetryBootstrapBody("codex_app", "automation_update", opsRetryAutomationBootstrap)
	bootstrapWithPreviousResponse := strings.Replace(
		validBootstrap,
		`"model":"gpt-5"`,
		`"model":"gpt-5","previous_response_id":"resp_1"`,
		1,
	)
	tests := []struct {
		name string
		mode string
		log  OpsErrorLogDetail
	}{
		{name: "upstream event", mode: OpsRetryModeUpstreamEvent, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/responses"}, RequestBody: validBootstrap}},
		{name: "truncated", mode: OpsRetryModeClient, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/responses"}, RequestBody: validBootstrap, RequestBodyTruncated: true}},
		{name: "images", mode: OpsRetryModeClient, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/images/generations"}, RequestBody: validBootstrap}},
		{name: "compact subresource", mode: OpsRetryModeClient, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/responses/compact"}, RequestBody: validBootstrap}},
		{name: "cancel subresource", mode: OpsRetryModeClient, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/responses/resp_1/cancel"}, RequestBody: validBootstrap}},
		{name: "unsafe previous response", mode: OpsRetryModeClient, log: OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestPath: "/v1/responses"}, RequestBody: bootstrapWithPreviousResponse}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := prepareOpsRetryBody(test.mode, &test.log)
			require.True(t, bytes.Equal([]byte(test.log.RequestBody), got))
		})
	}
}

func TestIsOpenAIResponsesCreatePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/v1/responses", want: true},
		{path: "/v1/responses/", want: true},
		{path: "/openai/v1/responses", want: true},
		{path: "/responses", want: true},
		{path: "/backend-api/codex/responses", want: true},
		{path: "/v1/responses/compact", want: false},
		{path: "/v1/responses/resp_1/cancel", want: false},
		{path: "/other/responses", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			require.Equal(t, test.want, IsOpenAIResponsesCreatePath(test.path))
		})
	}
}
