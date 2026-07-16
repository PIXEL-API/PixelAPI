package service

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

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
