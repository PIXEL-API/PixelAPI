package openaiusage

import (
	"strings"

	"github.com/tidwall/gjson"
)

const EnvelopeCount = 4

type Envelope struct {
	Index       int
	Usage       gjson.Result
	Container   gjson.Result
	ImageGen    gjson.Result
	ServiceTier string
}

type envelopeCandidate struct {
	containerPath string
	usagePath     string
}

var envelopeCandidates = [EnvelopeCount]envelopeCandidate{
	{usagePath: "usage"},
	{containerPath: "response", usagePath: "response.usage"},
	{containerPath: "data", usagePath: "data.usage"},
	{containerPath: "data.response", usagePath: "data.response.usage"},
}

// SelectEnvelope returns the first valid usage object in the canonical
// precedence order. An empty object is valid and intentionally prevents lower
// priority envelopes from being selected; non-object JSON values are skipped.
func SelectEnvelope(body []byte) (Envelope, bool) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return Envelope{}, false
	}
	root := gjson.ParseBytes(body)
	for index, candidate := range envelopeCandidates {
		usage := root.Get(candidate.usagePath)
		if !usage.Exists() || !usage.IsObject() {
			continue
		}
		container := root
		if candidate.containerPath != "" {
			container = root.Get(candidate.containerPath)
		}
		return Envelope{
			Index:       index,
			Usage:       usage,
			Container:   container,
			ImageGen:    container.Get("tool_usage.image_gen"),
			ServiceTier: strings.TrimSpace(container.Get("service_tier").String()),
		}, true
	}
	return Envelope{}, false
}

// FirstPresentUsage returns the first candidate value that is explicitly
// present, regardless of its JSON shape. It is intended for diagnostics after
// SelectEnvelope reports that no valid object exists.
func FirstPresentUsage(body []byte) (gjson.Result, bool) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return gjson.Result{}, false
	}
	root := gjson.ParseBytes(body)
	for _, candidate := range envelopeCandidates {
		usage := root.Get(candidate.usagePath)
		if usage.Exists() {
			return usage, true
		}
	}
	return gjson.Result{}, false
}

type HostedImageGenTokens struct {
	InputTokens       int
	TextInputTokens   int
	ImageInputTokens  int
	OutputTokens      int
	TextOutputTokens  int
	ImageOutputTokens int
}

// ParseHostedImageGenTokens reads the separately reported hosted image tool
// usage. Reported totals are never allowed below their known text/image parts.
func ParseHostedImageGenTokens(imageGen gjson.Result) HostedImageGenTokens {
	if !imageGen.Exists() || !imageGen.IsObject() {
		return HostedImageGenTokens{}
	}
	imageInput := nonNegative(int(imageGen.Get("input_tokens_details.image_tokens").Int()))
	textInput := nonNegative(int(imageGen.Get("input_tokens_details.text_tokens").Int()))
	imageOutput := nonNegative(int(imageGen.Get("output_tokens_details.image_tokens").Int()))
	textOutput := nonNegative(int(imageGen.Get("output_tokens_details.text_tokens").Int()))
	return HostedImageGenTokens{
		InputTokens:       totalTokens(int(imageGen.Get("input_tokens").Int()), imageInput, textInput),
		TextInputTokens:   textInput,
		ImageInputTokens:  imageInput,
		OutputTokens:      totalTokens(int(imageGen.Get("output_tokens").Int()), imageOutput, textOutput),
		TextOutputTokens:  textOutput,
		ImageOutputTokens: imageOutput,
	}
}

func totalTokens(reported, image, text int) int {
	total := nonNegative(reported)
	if classified := image + text; classified > total {
		return classified
	}
	return total
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
