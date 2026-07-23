package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIFirstOutputStartPreservesEndToEndRequestStart(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	ctx := WithOpenAIFirstOutputStart(context.Background(), start)
	recovered := openAIFirstOutputStart(ctx)
	require.Equal(t, start, recovered)

	// Contexts without the marker retain the compatibility behavior and start
	// timing at the service entry point.
	compatStart := openAIFirstOutputStart(context.Background())
	require.WithinDuration(t, time.Now(), compatStart, time.Second)
}

func TestEnsureOpenAIFirstOutputStartIsStableForDirectServiceCallers(t *testing.T) {
	ctx, first := ensureOpenAIFirstOutputStart(context.Background())
	recovered := openAIFirstOutputStart(ctx)
	require.Equal(t, first, recovered)

	ctx2, second := ensureOpenAIFirstOutputStart(ctx)
	require.Same(t, ctx, ctx2)
	require.Equal(t, first, second)
}

func TestOpenAIFirstOutputRoutingBudgetDoesNotPenalizeAccount(t *testing.T) {
	err := (&OpenAIGatewayService{}).newOpenAIFirstOutputTimeoutError(
		context.Background(),
		nil,
		&Account{ID: 1, Platform: PlatformOpenAI},
		time.Now().Add(-time.Second),
		"gpt-5",
		"",
		time.Second,
		openAIFirstOutputPhaseRoutingBudget,
		nil,
	)
	require.Equal(t, GatewayFailureReasonRoutingBudgetExhausted, err.Reason)
	require.False(t, err.ShouldReportAccountScheduleFailure())
	require.False(t, err.ShouldRetryNextAccount())
}

func TestOpenAIFirstOutputStageUnlinkFailureFailsFastAndRetriesCleanup(t *testing.T) {
	stage := newDefaultOpenAIFirstOutputStage()
	stage.memoryOnly = false
	stage.createTemp = func() (*os.File, error) {
		return os.CreateTemp("", "sub2api-openai-first-output-fail-fast-*")
	}

	removeCalls := 0
	stage.removeFile = func(path string) error {
		removeCalls++
		if removeCalls <= 2 {
			return errors.New("forced remove failure")
		}
		return os.Remove(path)
	}

	_, err := stage.Write(bytes.Repeat([]byte("sensitive"), 9*1024))
	require.ErrorContains(t, err, "unlink first-output spool before use")
	require.False(t, stage.memoryOnly, "unlink failure must not silently switch storage strategy")
	require.Zero(t, stage.Buffered())
	require.Nil(t, stage.tempFile)
	require.NotEmpty(t, stage.tempPath)

	path := stage.tempPath
	stat, statErr := os.Stat(path)
	require.NoError(t, statErr)
	require.Zero(t, stat.Size(), "failed unlink must not leave request data in a named file")

	require.NoError(t, stage.Close())
	require.Empty(t, stage.tempPath)
	_, statErr = os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestOpenAIFirstOutputStageCreateTempFailureFailsFast(t *testing.T) {
	stage := newDefaultOpenAIFirstOutputStage()
	stage.memoryOnly = false
	stage.createTemp = func() (*os.File, error) {
		return nil, errors.New("forced create failure")
	}

	_, err := stage.Write(bytes.Repeat([]byte("x"), openAIFirstOutputStageMemoryLimit+1))
	require.ErrorContains(t, err, "create first-output spool")
	require.Zero(t, stage.Buffered())
	require.NoError(t, stage.Close())
}
