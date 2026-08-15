package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

var grokResponsesPingComment = []byte(": ping\n\n")

const (
	grokResponsesPingFrameMaxLines = 16
	grokResponsesPingFrameMaxBytes = 16 * 1024
)

type grokResponsesBillingPingFilterBody struct {
	*io.PipeReader
	source    io.Closer
	closeOnce sync.Once
	closeErr  error
}

func (b *grokResponsesBillingPingFilterBody) Close() error {
	readerErr := b.PipeReader.Close()
	sourceErr := b.closeSource()
	if readerErr != nil {
		return readerErr
	}
	return sourceErr
}

func (b *grokResponsesBillingPingFilterBody) closeSource() error {
	b.closeOnce.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}

func newGrokResponsesBillingPingFilterBody(source io.ReadCloser, account *Account, maxLineSize int) io.ReadCloser {
	if source == nil || account == nil || account.Platform != PlatformGrok {
		return source
	}
	reader, writer := io.Pipe()
	body := &grokResponsesBillingPingFilterBody{PipeReader: reader, source: source}
	go filterGrokResponsesBillingPings(source, writer, body.closeSource, maxLineSize)
	return body
}

func filterGrokResponsesBillingPings(
	source io.Reader,
	destination *io.PipeWriter,
	closeSource func() error,
	maxLineSize int,
) {
	defer func() { _ = closeSource() }()
	if maxLineSize <= 0 {
		maxLineSize = defaultMaxLineSize
	}

	scanner := bufio.NewScanner(source)
	scanBuf := getSSEScannerBuf64K()
	defer putSSEScannerBuf64K(scanBuf)
	initialBufferSize := len(scanBuf)
	if maxLineSize < initialBufferSize {
		initialBufferSize = maxLineSize
	}
	scanner.Buffer(scanBuf[:0:initialBufferSize], maxLineSize)
	scanner.Split(scanSSELinesPreservingEndings)

	pingFrame := make([][]byte, 0, 3)
	pingFrameBytes := 0
	inPassthroughFrame := false

	replayPingFrame := func() error {
		for _, line := range pingFrame {
			if _, err := destination.Write(line); err != nil {
				return err
			}
		}
		pingFrame = pingFrame[:0]
		pingFrameBytes = 0
		return nil
	}
	endPingFrame := func(blankLine []byte) error {
		if isGrokResponsesPingEventFrame(pingFrame) {
			pingFrame = pingFrame[:0]
			pingFrameBytes = 0
			_, err := destination.Write(grokResponsesPingComment)
			return err
		}
		if err := replayPingFrame(); err != nil {
			return err
		}
		if blankLine == nil {
			return nil
		}
		_, err := destination.Write(blankLine)
		return err
	}
	abort := func(err error) { _ = destination.CloseWithError(err) }

	for scanner.Scan() {
		line := scanner.Bytes()
		isBlank := len(trimSSELineEnding(line)) == 0

		if inPassthroughFrame {
			if _, err := destination.Write(line); err != nil {
				abort(err)
				return
			}
			if isBlank {
				inPassthroughFrame = false
			}
			continue
		}

		if len(pingFrame) > 0 {
			if isBlank {
				if err := endPingFrame(line); err != nil {
					abort(err)
					return
				}
				continue
			}
			if canExtendGrokResponsesPingFrame(line) &&
				len(pingFrame) < grokResponsesPingFrameMaxLines &&
				pingFrameBytes+len(line) <= grokResponsesPingFrameMaxBytes {
				pingFrame = append(pingFrame, append([]byte(nil), line...))
				pingFrameBytes += len(line)
				continue
			}
			if err := replayPingFrame(); err != nil {
				abort(err)
				return
			}
			if _, err := destination.Write(line); err != nil {
				abort(err)
				return
			}
			inPassthroughFrame = true
			continue
		}

		if !isBlank {
			if value, ok := extractOpenAISSEEventLine(string(trimSSELineEnding(line))); ok && value == "ping" {
				pingFrame = append(pingFrame, append([]byte(nil), line...))
				pingFrameBytes = len(line)
				continue
			}
		}
		if _, err := destination.Write(line); err != nil {
			abort(err)
			return
		}
		inPassthroughFrame = !isBlank
	}
	if len(pingFrame) > 0 {
		if err := endPingFrame(nil); err != nil {
			abort(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		abort(fmt.Errorf("filter Grok Responses billing ping: %w", err))
		return
	}
	_ = destination.Close()
}

func scanSSELinesPreservingEndings(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for index, value := range data {
		switch value {
		case '\n':
			return index + 1, data[:index+1], nil
		case '\r':
			if index+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			if index+1 < len(data) && data[index+1] == '\n' {
				return index + 2, data[:index+2], nil
			}
			return index + 1, data[:index+1], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func trimSSELineEnding(line []byte) []byte {
	return bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
}

func canExtendGrokResponsesPingFrame(rawLine []byte) bool {
	line := trimSSELineEnding(rawLine)
	if len(line) > 0 && line[0] == ':' {
		return true
	}
	_, ok := extractOpenAISSEDataLine(string(line))
	return ok
}

func isGrokResponsesPingEventFrame(rawLines [][]byte) bool {
	dataParts := make([]string, 0, 1)
	for _, rawLine := range rawLines[1:] {
		if value, ok := extractOpenAISSEDataLine(string(trimSSELineEnding(rawLine))); ok {
			dataParts = append(dataParts, value)
		}
	}
	if len(dataParts) == 0 {
		return true
	}
	var payload struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal([]byte(strings.Join(dataParts, "\n")), &payload); err != nil || payload.Type == nil {
		return true
	}
	return *payload.Type == "ping"
}
