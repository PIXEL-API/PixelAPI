package service

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

func TestExtractContentModerationInputUsesBoundedCollectorAcrossProtocols(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("用户输入 ", maxModerationInputRunes*2)
	cases := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "anthropic",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"` + longText + `"},{"type":"image","source":{"media_type":"image/png","data":"QUJD"}}]}]}`,
		},
		{
			name:     "openai chat",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"` + longText + `"},{"type":"image_url","image_url":{"url":"https://example.test/one.png"}},{"type":"image_url","image_url":{"url":"https://example.test/two.png"}}]}]}`,
		},
		{
			name:     "gemini",
			protocol: ContentModerationProtocolGemini,
			body:     `{"contents":[{"role":"user","parts":[{"text":"` + longText + `"},{"inline_data":{"mime_type":"image/png","data":"QUJD"}},{"inline_data":{"mime_type":"image/png","data":"REVG"}}]}]}`,
		},
		{
			name:     "openai images",
			protocol: ContentModerationProtocolOpenAIImages,
			body:     `{"prompt":"` + longText + `","images":[{"type":"image_url","image_url":{"url":"https://example.test/one.png"}},{"type":"image_url","image_url":{"url":"https://example.test/two.png"}}]}`,
		},
		{
			name:     "openai responses",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"` + longText + `"},{"type":"input_image","image_url":"https://example.test/one.png"},{"type":"input_image","image_url":"https://example.test/two.png"}]}]}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := ExtractContentModerationInput(tc.protocol, []byte(tc.body))
			if got := utf8.RuneCountInString(content.Text); got > maxModerationInputRunes {
				t.Fatalf("text rune count = %d, want <= %d", got, maxModerationInputRunes)
			}
			if len(content.Images) > maxContentModerationInputImages {
				t.Fatalf("selected images = %d, want <= %d", len(content.Images), maxContentModerationInputImages)
			}
			if len(content.allImageDigests) == 0 {
				t.Fatal("image digest list is empty for a request containing images")
			}
		})
	}
}

func TestExtractContentModerationInputKeepsLastNonEmptyMessageAndDeduplicatesImages(t *testing.T) {
	t.Parallel()

	body := []byte(`{"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"old"},{"type":"image_url","image_url":{"url":"https://example.test/old.png"}}]},` +
		`{"role":"assistant","content":"ignored"},` +
		`{"role":"user","content":[{"type":"text","text":"latest"},{"type":"image_url","image_url":{"url":"https://example.test/one.png"}},{"type":"image_url","image_url":{"url":"https://example.test/one.png"}},{"type":"image_url","image_url":{"url":"https://example.test/two.png"}}]` +
		`}]}`)

	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)
	if !strings.Contains(content.Text, "latest") || strings.Contains(content.Text, "old") {
		t.Fatalf("text = %q, want only the last non-empty user message", content.Text)
	}
	if got := len(content.allImageDigests); got != 2 {
		t.Fatalf("unique image digests = %d, want 2", got)
	}
	if len(content.Images) != 1 || (content.Images[0] != "https://example.test/one.png" && content.Images[0] != "https://example.test/two.png") {
		t.Fatalf("selected image = %#v, want one of latest message images", content.Images)
	}
	want := ContentModerationInput{
		Text:   "latest",
		Images: []string{"https://example.test/one.png", "https://example.test/two.png"},
	}
	if got, expected := content.Hash(), want.Hash(); got != expected {
		t.Fatalf("hash = %s, want %s (all unique image digests must be preserved)", got, expected)
	}
}

func TestExtractContentModerationInputEmptyAndInvalidAreBounded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		protocol string
		body     []byte
	}{
		{name: "empty body", protocol: ContentModerationProtocolOpenAIChat, body: nil},
		{name: "invalid json", protocol: ContentModerationProtocolAnthropicMessages, body: []byte("{")},
		{name: "valid empty object", protocol: ContentModerationProtocolGemini, body: []byte(`{}`)},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractContentModerationInput(tc.protocol, tc.body); !got.IsEmpty() {
				t.Fatalf("input = %#v, want empty", got)
			}
		})
	}
}

func TestExtractContentModerationInputBoundsImageDigestMetadata(t *testing.T) {
	t.Parallel()

	const imageCount = maxModerationImageDigestEntries + 257
	var body strings.Builder
	body.WriteString(`{"messages":[{"role":"user","content":[`)
	for i := 0; i < imageCount; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&body, `{"type":"image_url","image_url":{"url":"https://example.test/%d.png"}}`, i)
	}
	body.WriteString(`]}]}`)

	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(body.String()))
	if got := len(content.allImageDigests); got != maxModerationImageDigestEntries {
		t.Fatalf("exact image digests = %d, want bounded prefix %d", got, maxModerationImageDigestEntries)
	}
	if got, want := content.imageDigestOverflowCount, uint64(imageCount-maxModerationImageDigestEntries); got != want {
		t.Fatalf("overflow image digest count = %d, want %d", got, want)
	}
	if len(content.Images) != maxContentModerationInputImages {
		t.Fatalf("selected images = %d, want %d", len(content.Images), maxContentModerationInputImages)
	}

	again := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(body.String()))
	if content.Hash() != again.Hash() {
		t.Fatalf("overflow hash is not deterministic: %s != %s", content.Hash(), again.Hash())
	}
}

func TestExtractContentModerationInputDoesNotRetainRequestBodyThroughImageView(t *testing.T) {
	t.Parallel()

	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"audit me"},{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)
	if len(content.Images) != 1 {
		t.Fatalf("selected images = %d, want 1", len(content.Images))
	}

	bodyStart := uintptr(unsafe.Pointer(unsafe.SliceData(body)))
	bodyEnd := bodyStart + uintptr(len(body))
	imageStart := uintptr(unsafe.Pointer(unsafe.StringData(content.Images[0])))
	if imageStart >= bodyStart && imageStart < bodyEnd {
		t.Fatal("selected image aliases the request body and would retain the complete body")
	}
	textStart := uintptr(unsafe.Pointer(unsafe.StringData(content.Text)))
	if textStart >= bodyStart && textStart < bodyEnd {
		t.Fatal("moderation text aliases the request body and would retain the complete body")
	}
}
