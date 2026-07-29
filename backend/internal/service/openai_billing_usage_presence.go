package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// openAIResponsesBillingUsageObservation tracks raw Responses usage fields
// without inferring presence from the decoded integer values. Top-level usage
// and response.usage are tracked separately so fields from different usage
// objects cannot accidentally form a complete pair.
type openAIResponsesBillingUsageObservation struct {
	topLevelUsage billingUsageObservation
	responseUsage billingUsageObservation
}

func (o *openAIResponsesBillingUsageObservation) observePayload(payload []byte) {
	if o == nil || len(payload) == 0 || !gjson.ValidBytes(payload) {
		return
	}
	parsed := gjson.ParseBytes(payload)
	observeOpenAIBillingUsagePair(&o.topLevelUsage, parsed.Get("usage"), "input_tokens", "output_tokens")
	observeOpenAIBillingUsagePair(&o.responseUsage, parsed.Get("response.usage"), "input_tokens", "output_tokens")
}

func (o openAIResponsesBillingUsageObservation) complete() bool {
	return o.topLevelUsage.complete() || o.responseUsage.complete()
}

type openAIChatCompletionsBillingUsageObservation struct {
	usage billingUsageObservation
}

func (o *openAIChatCompletionsBillingUsageObservation) observePayload(payload []byte) {
	if o == nil || len(payload) == 0 || !gjson.ValidBytes(payload) {
		return
	}
	parsed := gjson.ParseBytes(payload)
	observeOpenAIBillingUsagePair(&o.usage, parsed.Get("usage"), "prompt_tokens", "completion_tokens")
}

func (o openAIChatCompletionsBillingUsageObservation) complete() bool {
	return o.usage.complete()
}

func observeOpenAIBillingUsagePair(
	observation *billingUsageObservation,
	usage gjson.Result,
	inputField string,
	outputField string,
) {
	if observation == nil || !usage.Exists() || !usage.IsObject() {
		return
	}
	observation.inputTokensObserved = observation.inputTokensObserved ||
		billingTokenFieldObserved(usage.Get(inputField))
	observation.outputTokensObserved = observation.outputTokensObserved ||
		billingTokenFieldObserved(usage.Get(outputField))
}

func openAIResponsesBillingUsageComplete(body []byte) bool {
	var observation openAIResponsesBillingUsageObservation
	observeOpenAIResponsesBody(&observation, body)
	return observation.complete()
}

func openAIChatCompletionsBillingUsageComplete(body []byte) bool {
	var observation openAIChatCompletionsBillingUsageObservation
	observeOpenAIChatCompletionsBody(&observation, body)
	return observation.complete()
}

func observeOpenAIResponsesBody(observation *openAIResponsesBillingUsageObservation, body []byte) {
	if observation == nil || len(body) == 0 {
		return
	}
	if gjson.ValidBytes(body) {
		observation.observePayload(body)
		return
	}
	forEachOpenAISSEDataPayload(string(body), observation.observePayload)
}

func observeOpenAIChatCompletionsBody(observation *openAIChatCompletionsBillingUsageObservation, body []byte) {
	if observation == nil || len(body) == 0 {
		return
	}
	if gjson.ValidBytes(body) {
		observation.observePayload(body)
		return
	}
	forEachOpenAISSEDataPayload(strings.TrimSpace(string(body)), observation.observePayload)
}
