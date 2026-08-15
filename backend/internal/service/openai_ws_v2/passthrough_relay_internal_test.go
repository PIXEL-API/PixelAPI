package openai_ws_v2

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeRelayResponseModelBoundsUnicodeRunes(t *testing.T) {
	t.Parallel()

	model := normalizeRelayResponseModel("  " + strings.Repeat("模", relayResponseModelMaxLength+1) + "  ")
	require.Len(t, []rune(model), relayResponseModelMaxLength)
	require.Equal(t, strings.Repeat("模", relayResponseModelMaxLength), model)
	require.Equal(t, strings.Repeat("x", relayResponseModelMaxLength), normalizeRelayResponseModel(strings.Repeat("x", relayResponseModelMaxLength)))
}

func TestBuildRelayTurnResultResponseModelBillingEligibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		eventType string
		eligible  bool
	}{
		{eventType: "response.completed", eligible: true},
		{eventType: "response.done", eligible: true},
		{eventType: "response.failed", eligible: false},
		{eventType: "response.incomplete", eligible: false},
		{eventType: "response.cancelled", eligible: false},
		{eventType: "response.canceled", eligible: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.eventType, func(t *testing.T) {
			t.Parallel()
			result, ok := buildRelayTurnResult(nil, observedUpstreamEvent{
				terminal:      true,
				eventType:     test.eventType,
				responseID:    "resp_test",
				responseModel: "gpt-response",
			})
			require.True(t, ok)
			require.Equal(t, "gpt-response", result.ResponseModel)
			require.Equal(t, test.eligible, result.ResponseModelBillingEligible)
		})
	}

	withoutModel, ok := buildRelayTurnResult(nil, observedUpstreamEvent{terminal: true, eventType: "response.completed", responseID: "resp_empty"})
	require.True(t, ok)
	require.False(t, withoutModel.ResponseModelBillingEligible)
}

func TestRunEntry_DelegatesRelay(t *testing.T) {
	t.Parallel()

	clientConn := newPassthroughTestFrameConn(nil, false)
	upstreamConn := newPassthroughTestFrameConn([]passthroughTestFrame{
		{
			msgType: coderws.MessageText,
			payload: []byte(`{"type":"response.completed","response":{"id":"resp_entry","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
	}, true)

	result, relayExit := RunEntry(EntryInput{
		Ctx:                context.Background(),
		ClientConn:         clientConn,
		UpstreamConn:       upstreamConn,
		FirstClientMessage: []byte(`{"type":"response.create","model":"gpt-4o","input":[]}`),
	})
	require.Nil(t, relayExit)
	require.Equal(t, "resp_entry", result.RequestID)
}

func TestRunClientToUpstream_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("read client eof", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		runClientToUpstream(
			context.Background(),
			newPassthroughTestFrameConn(nil, true),
			func(_ coderws.MessageType, _ []byte) error { return nil },
			func() {},
			nil,
			nil,
			nil,
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "read_client", sig.stage)
		require.True(t, sig.graceful)
	})

	t.Run("write upstream failed", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		runClientToUpstream(
			context.Background(),
			newPassthroughTestFrameConn([]passthroughTestFrame{
				{msgType: coderws.MessageText, payload: []byte(`{"x":1}`)},
			}, true),
			func(_ coderws.MessageType, _ []byte) error { return errors.New("boom") },
			func() {},
			nil,
			nil,
			nil,
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "write_upstream", sig.stage)
		require.False(t, sig.graceful)
	})

	t.Run("forwarded counter and trace callback", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		forwarded := &atomic.Int64{}
		traces := make([]RelayTraceEvent, 0, 2)
		runClientToUpstream(
			context.Background(),
			newPassthroughTestFrameConn([]passthroughTestFrame{
				{msgType: coderws.MessageText, payload: []byte(`{"x":1}`)},
			}, true),
			func(_ coderws.MessageType, _ []byte) error { return nil },
			func() {},
			forwarded,
			nil,
			func(event RelayTraceEvent) {
				traces = append(traces, event)
			},
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "read_client", sig.stage)
		require.Equal(t, int64(1), forwarded.Load())
		require.NotEmpty(t, traces)
	})
}

func TestRunUpstreamToClient_ErrorAndDropPaths(t *testing.T) {
	t.Parallel()

	t.Run("read upstream eof", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		drop := &atomic.Bool{}
		drop.Store(false)
		runUpstreamToClient(
			context.Background(),
			newPassthroughTestFrameConn(nil, true),
			func(_ coderws.MessageType, _ []byte) error { return nil },
			time.Now(),
			time.Now,
			&relayState{},
			nil,
			nil,
			nil,
			drop,
			nil,
			nil,
			func() {},
			nil,
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "read_upstream", sig.stage)
		require.True(t, sig.graceful)
	})

	t.Run("write client failed", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		drop := &atomic.Bool{}
		drop.Store(false)
		runUpstreamToClient(
			context.Background(),
			newPassthroughTestFrameConn([]passthroughTestFrame{
				{msgType: coderws.MessageText, payload: []byte(`{"type":"response.output_text.delta","delta":"x"}`)},
			}, true),
			func(_ coderws.MessageType, _ []byte) error { return errors.New("write failed") },
			time.Now(),
			time.Now,
			&relayState{},
			nil,
			nil,
			nil,
			drop,
			nil,
			nil,
			func() {},
			nil,
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "write_client", sig.stage)
	})

	t.Run("drop downstream and stop on terminal", func(t *testing.T) {
		t.Parallel()

		exitCh := make(chan relayExitSignal, 1)
		drop := &atomic.Bool{}
		drop.Store(true)
		dropped := &atomic.Int64{}
		runUpstreamToClient(
			context.Background(),
			newPassthroughTestFrameConn([]passthroughTestFrame{
				{
					msgType: coderws.MessageText,
					payload: []byte(`{"type":"response.completed","response":{"id":"resp_drop","usage":{"input_tokens":1,"output_tokens":1}}}`),
				},
			}, true),
			func(_ coderws.MessageType, _ []byte) error { return nil },
			time.Now(),
			time.Now,
			&relayState{},
			nil,
			nil,
			nil,
			drop,
			nil,
			dropped,
			func() {},
			nil,
			exitCh,
		)
		sig := <-exitCh
		require.Equal(t, "drain_terminal", sig.stage)
		require.True(t, sig.graceful)
		require.Equal(t, int64(1), dropped.Load())
	})
}

func TestRunIdleWatchdog_NoTimeoutWhenDisabled(t *testing.T) {
	t.Parallel()

	exitCh := make(chan relayExitSignal, 1)
	lastActivity := &atomic.Int64{}
	lastActivity.Store(time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runIdleWatchdog(ctx, time.Now, 0, lastActivity, nil, exitCh)
	select {
	case <-exitCh:
		t.Fatal("unexpected idle timeout signal")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHelperFunctionsCoverage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "text", relayMessageTypeString(coderws.MessageText))
	require.Equal(t, "binary", relayMessageTypeString(coderws.MessageBinary))
	require.Contains(t, relayMessageTypeString(coderws.MessageType(99)), "unknown(")

	require.Equal(t, "", relayErrorString(nil))
	require.Equal(t, "x", relayErrorString(errors.New("x")))

	require.True(t, isDisconnectError(io.EOF))
	require.True(t, isDisconnectError(net.ErrClosed))
	require.True(t, isDisconnectError(context.Canceled))
	require.True(t, isDisconnectError(coderws.CloseError{Code: coderws.StatusGoingAway}))
	require.True(t, isDisconnectError(errors.New("broken pipe")))
	require.False(t, isDisconnectError(errors.New("unrelated")))

	require.True(t, isTokenEvent("response.output_text.delta"))
	require.True(t, isTokenEvent("response.output_audio.delta"))
	require.True(t, isTokenEvent("response.completed"))
	require.False(t, isTokenEvent(""))
	require.False(t, isTokenEvent("response.created"))

	require.Equal(t, 2*time.Second, minDuration(2*time.Second, 5*time.Second))
	require.Equal(t, 2*time.Second, minDuration(5*time.Second, 2*time.Second))
	require.Equal(t, 5*time.Second, minDuration(0, 5*time.Second))
	require.Equal(t, 2*time.Second, minDuration(2*time.Second, 0))

	ch := make(chan relayExitSignal, 1)
	ch <- relayExitSignal{stage: "ok"}
	sig, ok := waitRelayExit(ch, 10*time.Millisecond)
	require.True(t, ok)
	require.Equal(t, "ok", sig.stage)
	ch <- relayExitSignal{stage: "ok2"}
	sig, ok = waitRelayExit(ch, 0)
	require.True(t, ok)
	require.Equal(t, "ok2", sig.stage)
	_, ok = waitRelayExit(ch, 10*time.Millisecond)
	require.False(t, ok)

	n, ok := parseUsageIntField(gjson.Get(`{"n":3}`, "n"), true)
	require.True(t, ok)
	require.Equal(t, 3, n)
	_, ok = parseUsageIntField(gjson.Get(`{"n":"x"}`, "n"), true)
	require.False(t, ok)
	n, ok = parseUsageIntField(gjson.Result{}, false)
	require.True(t, ok)
	require.Equal(t, 0, n)
	_, ok = parseUsageIntField(gjson.Result{}, true)
	require.False(t, ok)
}

func TestParseUsageAndEnrichCoverage(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":"bad"}}}`), "response.completed", nil)
	require.Equal(t, 0, state.usage.InputTokens)

	parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":9,"output_tokens":"bad","input_tokens_details":{"cached_tokens":2}}}}`),
		"response.completed",
		nil,
	)
	require.Equal(t, 0, state.usage.InputTokens, "部分字段解析失败时不应累加 usage")
	require.Equal(t, 0, state.usage.OutputTokens)
	require.Equal(t, 0, state.usage.CacheReadInputTokens)

	parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens_details":{"cached_tokens":2}}}}`),
		"response.completed",
		nil,
	)
	require.Equal(t, 0, state.usage.InputTokens, "必填 usage 字段缺失时不应累加 usage")
	require.Equal(t, 0, state.usage.OutputTokens)
	require.Equal(t, 0, state.usage.CacheReadInputTokens)

	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":1,"input_tokens_details":{"cached_tokens":1}}}}`), "response.completed", nil)
	require.Equal(t, 2, state.usage.InputTokens)
	require.Equal(t, 1, state.usage.OutputTokens)
	require.Equal(t, 1, state.usage.CacheReadInputTokens)

	state.imageCounter = newImageOutputCounter()
	state.imageCounter.AddMessage([]byte(`{"type":"response.output_item.done","item":{"id":"ig_internal","type":"image_generation_call","result":"ZmluYWw="}}`))
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2,"output_tokens_details":{"image_tokens":4}}}}`), "response.completed", nil)
	require.Equal(t, 1, state.usage.ImageCount)
	require.Equal(t, 4, state.usage.ImageOutputTokens)

	result := &RelayResult{}
	enrichResult(result, state, 5*time.Millisecond)
	require.Equal(t, state.usage.InputTokens, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.ImageCount)
	require.Equal(t, 5*time.Millisecond, result.Duration)
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","response":{"usage":{"input_tokens":9}}}`), "response.in_progress", nil)
	require.Equal(t, 5, state.usage.InputTokens)
	enrichResult(nil, state, 0)
}

func TestOpenAIWSV2BillingUsageCompleteRequiresBothNonNegativeNumberFields(t *testing.T) {
	require.True(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
	)))
	require.False(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"input_tokens":1}}}`,
	)))
	require.False(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"output_tokens":1}}}`,
	)))
	require.False(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"input_tokens":-1,"output_tokens":1}}}`,
	)))
	require.False(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":"1"}}}`,
	)))
	require.False(t, openAIWSV2BillingUsageComplete([]byte(
		`{"type":"response.completed","response":{"usage":{"input_tokens":1.5,"output_tokens":2}}}`,
	)))
}

func TestParseUsageAndAccumulateSupportsUsageEnvelopePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantInput  int
		wantOutput int
		wantImage  int
		complete   bool
	}{
		{
			name:       "top level usage",
			message:    `{"type":"response.completed","usage":{"input_tokens":11,"output_tokens":7}}`,
			wantInput:  11,
			wantOutput: 7,
			complete:   true,
		},
		{
			name:       "response usage",
			message:    `{"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":8}}}`,
			wantInput:  12,
			wantOutput: 8,
			complete:   true,
		},
		{
			name:       "data usage",
			message:    `{"type":"response.done","data":{"usage":{"input_tokens":13,"output_tokens":9}}}`,
			wantInput:  13,
			wantOutput: 9,
			complete:   true,
		},
		{
			name:       "data response usage",
			message:    `{"type":"response.done","data":{"response":{"usage":{"input_tokens":14,"output_tokens":10},"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":3,"output_tokens_details":{"image_tokens":3}}}}}}`,
			wantInput:  21,
			wantOutput: 13,
			wantImage:  3,
			complete:   true,
		},
		{
			name:       "top level wins",
			message:    `{"type":"response.completed","usage":{"input_tokens":1,"output_tokens":2},"response":{"usage":{"input_tokens":100,"output_tokens":200}}}`,
			wantInput:  1,
			wantOutput: 2,
			complete:   true,
		},
		{
			name:       "empty top level object blocks lower priority",
			message:    `{"type":"response.completed","usage":{},"response":{"usage":{"input_tokens":100,"output_tokens":200}}}`,
			wantInput:  0,
			wantOutput: 0,
			complete:   false,
		},
		{
			name:       "invalid top level shape is skipped",
			message:    `{"type":"response.completed","usage":null,"response":{"usage":{"input_tokens":3,"output_tokens":4}}}`,
			wantInput:  3,
			wantOutput: 4,
			complete:   true,
		},
		{
			name:       "prompt completion aliases are billing complete",
			message:    `{"type":"response.completed","data":{"usage":{"prompt_tokens":5,"completion_tokens":6}}}`,
			wantInput:  5,
			wantOutput: 6,
			complete:   true,
		},
		{
			name:       "canonical explicit zero beats legacy aliases",
			message:    `{"type":"response.completed","usage":{"input_tokens":0,"prompt_tokens":5,"output_tokens":0,"completion_tokens":6}}`,
			wantInput:  0,
			wantOutput: 0,
			complete:   true,
		},
		{
			name:       "different envelopes cannot form a pair",
			message:    `{"type":"response.completed","usage":{"input_tokens":7},"response":{"usage":{"output_tokens":3}}}`,
			wantInput:  0,
			wantOutput: 0,
			complete:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &relayState{}
			parsed := parseUsageAndAccumulate(state, []byte(tt.message), strings.TrimSpace(gjson.Get(tt.message, "type").String()), nil)
			require.Equal(t, tt.wantInput, parsed.InputTokens)
			require.Equal(t, tt.wantOutput, parsed.OutputTokens)
			require.Equal(t, tt.wantImage, parsed.ImageOutputTokens)
			require.Equal(t, tt.wantInput, state.usage.InputTokens)
			require.Equal(t, tt.wantOutput, state.usage.OutputTokens)
			require.Equal(t, tt.wantImage, state.usage.ImageOutputTokens)
			require.Equal(t, tt.complete, openAIWSV2BillingUsageComplete([]byte(tt.message)))
		})
	}
}

func TestParseUsageAndAccumulateAcceptsEmptyUsageObjectWithoutParseFailure(t *testing.T) {
	state := &relayState{}
	parseFailures := 0

	parsed := parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","usage":{},"response":{"usage":"invalid"}}`),
		"response.completed",
		func(string, string) { parseFailures++ },
	)

	require.Equal(t, Usage{}, parsed)
	require.Equal(t, Usage{}, state.usage)
	require.Zero(t, parseFailures)
}

func TestObserveUpstreamMessageDoesNotAccumulateDuplicateTerminalSnapshot(t *testing.T) {
	now := time.Unix(100, 0)
	state := &relayState{}
	completed := []byte(`{"type":"response.completed","response":{"id":"resp_duplicate","usage":{"input_tokens":10,"output_tokens":5}}}`)
	done := []byte(`{"type":"response.done","response":{"id":"resp_duplicate","usage":{"input_tokens":10,"output_tokens":5}}}`)

	observeUpstreamMessage(state, completed, now, func() time.Time { return now }, nil)
	observeUpstreamMessage(state, done, now, func() time.Time { return now }, nil)

	require.Equal(t, 10, state.usage.InputTokens)
	require.Equal(t, 5, state.usage.OutputTokens)
}

func TestObserveUpstreamMessageKeepsFirstTerminalSnapshot(t *testing.T) {
	now := time.Unix(100, 0)
	state := &relayState{}
	first := []byte(`{"type":"response.completed","response":{"id":"resp_corrected","usage":{"input_tokens":10,"output_tokens":5}}}`)
	corrected := []byte(`{"type":"response.done","response":{"id":"resp_corrected","usage":{"input_tokens":12,"output_tokens":6}}}`)

	firstObserved := observeUpstreamMessage(state, first, now, func() time.Time { return now }, nil)
	duplicateObserved := observeUpstreamMessage(state, corrected, now, func() time.Time { return now }, nil)

	require.True(t, firstObserved.terminal)
	require.False(t, firstObserved.duplicateTerminal)
	require.True(t, duplicateObserved.terminal)
	require.True(t, duplicateObserved.duplicateTerminal)
	require.Equal(t, 10, state.usage.InputTokens)
	require.Equal(t, 5, state.usage.OutputTokens)
}

func TestRelayStateTerminalResponseIDDigestSetDistinguishesIDsWithoutRetainingRawValues(t *testing.T) {
	state := &relayState{}
	firstID := "resp_terminal_exact_a"
	secondID := "resp_terminal_exact_b"

	state.rememberTerminalResponseID(firstID)
	state.rememberTerminalResponseID(secondID)

	require.True(t, state.hasTerminalResponseID(firstID))
	require.True(t, state.hasTerminalResponseID(secondID))
	require.False(t, state.hasTerminalResponseID("resp_terminal_exact_c"))
	require.Len(t, state.terminalResponseIDs, 2)
	for storedHash := range state.terminalResponseIDs {
		require.NotContains(t, string(storedHash[:]), firstID)
		require.NotContains(t, string(storedHash[:]), secondID)
	}
}

func TestEmitTurnCompleteCoverage(t *testing.T) {
	t.Parallel()

	// 非 terminal 事件不应触发。
	called := 0
	emitTurnComplete(func(turn RelayTurnResult) {
		called++
	}, &relayState{requestModel: "gpt-5"}, observedUpstreamEvent{
		terminal:   false,
		eventType:  "response.output_text.delta",
		responseID: "resp_ignored",
		usage:      Usage{InputTokens: 1},
	})
	require.Equal(t, 0, called)

	// 缺少 response_id 时不应触发。
	emitTurnComplete(func(turn RelayTurnResult) {
		called++
	}, &relayState{requestModel: "gpt-5"}, observedUpstreamEvent{
		terminal:  true,
		eventType: "response.completed",
	})
	require.Equal(t, 0, called)

	// terminal 且 response_id 存在，应该触发；state=nil 时 model 为空串。
	var got RelayTurnResult
	emitTurnComplete(func(turn RelayTurnResult) {
		called++
		got = turn
	}, nil, observedUpstreamEvent{
		terminal:   true,
		eventType:  "response.completed",
		responseID: "resp_emit",
		usage:      Usage{InputTokens: 2, OutputTokens: 3},
	})
	require.Equal(t, 1, called)
	require.Equal(t, "resp_emit", got.RequestID)
	require.Equal(t, "response.completed", got.TerminalEventType)
	require.Equal(t, 2, got.Usage.InputTokens)
	require.Equal(t, 3, got.Usage.OutputTokens)
	require.Equal(t, "", got.RequestModel)
}

func TestEmitTurnCompleteUsesPerTurnImageDelta(t *testing.T) {
	t.Parallel()

	state := &relayState{
		requestModel: "gpt-5.4",
		imageCounter: newImageOutputCounter(),
	}
	var imageCounts []int
	onComplete := func(turn RelayTurnResult) {
		imageCounts = append(imageCounts, turn.Usage.ImageCount)
	}
	emit := func(responseID string) {
		emitTurnComplete(onComplete, state, observedUpstreamEvent{
			terminal:   true,
			eventType:  "response.completed",
			responseID: responseID,
			usage:      Usage{},
		})
	}

	state.imageCounter.AddMessage([]byte(`{"type":"response.output_item.done","item":{"id":"ig_turn_1","type":"image_generation_call","result":"MQ=="}}`))
	emit("resp_turn_1")

	state.imageCounter.AddMessage([]byte(`{"type":"response.output_item.done","item":{"id":"ig_turn_2","type":"image_generation_call","result":"Mg=="}}`))
	emit("resp_turn_2")

	emit("resp_turn_3")

	require.Equal(t, []int{1, 1, 0}, imageCounts)
	require.Equal(t, 2, state.settledImageCount)
}

func TestIsDisconnectErrorCoverage_CloseStatusesAndMessageBranches(t *testing.T) {
	t.Parallel()

	require.True(t, isDisconnectError(coderws.CloseError{Code: coderws.StatusNormalClosure}))
	require.True(t, isDisconnectError(coderws.CloseError{Code: coderws.StatusNoStatusRcvd}))
	require.True(t, isDisconnectError(coderws.CloseError{Code: coderws.StatusAbnormalClosure}))
	require.True(t, isDisconnectError(errors.New("connection reset by peer")))
	require.False(t, isDisconnectError(errors.New("   ")))
}

func TestIsTokenEventCoverageBranches(t *testing.T) {
	t.Parallel()

	require.False(t, isTokenEvent("response.in_progress"))
	require.False(t, isTokenEvent("response.output_item.added"))
	require.True(t, isTokenEvent("response.output_audio.delta"))
	require.True(t, isTokenEvent("response.output"))
	require.True(t, isTokenEvent("response.done"))
}

func TestRelayTurnTimingHelpersCoverage(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	// nil state
	require.Nil(t, openAIWSRelayGetOrInitTurnTiming(nil, "resp_nil", now))
	_, ok := openAIWSRelayDeleteTurnTiming(nil, "resp_nil")
	require.False(t, ok)

	state := &relayState{}
	timing := openAIWSRelayGetOrInitTurnTiming(state, "resp_a", now)
	require.NotNil(t, timing)
	require.Equal(t, now, timing.startAt)

	// 再次获取返回同一条 timing
	timing2 := openAIWSRelayGetOrInitTurnTiming(state, "resp_a", now.Add(5*time.Second))
	require.NotNil(t, timing2)
	require.Equal(t, now, timing2.startAt)

	// 删除存在键
	deleted, ok := openAIWSRelayDeleteTurnTiming(state, "resp_a")
	require.True(t, ok)
	require.Equal(t, now, deleted.startAt)

	// 删除不存在键
	_, ok = openAIWSRelayDeleteTurnTiming(state, "resp_a")
	require.False(t, ok)
}

func TestObserveUpstreamMessage_ResponseIDFallbackPolicy(t *testing.T) {
	t.Parallel()

	state := &relayState{requestModel: "gpt-5"}
	startAt := time.Unix(0, 0)
	now := startAt
	nowFn := func() time.Time {
		now = now.Add(5 * time.Millisecond)
		return now
	}

	// 非 terminal：仅有顶层 id，不应把 event id 当成 response_id。
	observed := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.output_text.delta","id":"evt_123","delta":"hi"}`),
		startAt,
		nowFn,
		nil,
	)
	require.False(t, observed.terminal)
	require.Equal(t, "", observed.responseID)

	// terminal：允许兜底用顶层 id（用于兼容少数字段变体）。
	observed = observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","id":"resp_fallback","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.True(t, observed.terminal)
	require.Equal(t, "resp_fallback", observed.responseID)

	// data.response Usage 的伴随 response id 必须保持在同一自然 envelope 中。
	observed = observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.done","data":{"response":{"id":"resp_data_nested","usage":{"input_tokens":2,"output_tokens":1}}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.True(t, observed.terminal)
	require.Equal(t, "resp_data_nested", observed.responseID)
}
