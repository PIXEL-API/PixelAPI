package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	upstreamResponseModelObserverContextKey = "upstream_response_model_observer"
	upstreamResponseModelMaxLength          = 200
)

// upstreamResponseModelObserver tracks one forwarding attempt (or one WS turn).
// A terminal declaration wins over an earlier declaration; otherwise the first
// declaration is retained. Observation never affects the forwarding path.
//
// Billing normally ignores the observed model as well; the only exception is a
// channel explicitly configured with billing_model_source = response_model,
// where a conflict flag makes billing fall back to the baseline model
// (see responseModelBillingDeclaration).
type upstreamResponseModelObserver struct {
	first            string
	terminal         string
	conflict         bool
	billingEligible  bool
	billingRejected  bool
	protocolComplete bool
}

func (o *upstreamResponseModelObserver) Observe(model string, terminal bool) {
	if o == nil {
		return
	}
	model = normalizeObservedUpstreamResponseModel(model)
	if model == "" {
		return
	}
	current := o.Model()
	if current != "" && !strings.EqualFold(current, model) {
		o.conflict = true
	}
	if terminal {
		o.terminal = model
		return
	}
	if o.first == "" {
		o.first = model
	}
}

func normalizeObservedUpstreamResponseModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	runes := []rune(model)
	if len(runes) > upstreamResponseModelMaxLength {
		model = string(runes[:upstreamResponseModelMaxLength])
	}
	return model
}

func (o *upstreamResponseModelObserver) ObserveOpenAI(payload []byte, eventType string) {
	eventType = openAIResponseModelEventType(payload, eventType)
	model := firstValidTrimmedGJSONModel(payload, "response.model", "model")
	o.Observe(model, isUpstreamResponseModelTerminalEvent(eventType))
	if isUpstreamResponseModelSuccessfulTerminalEvent(eventType) {
		o.MarkBillingEligible()
	} else if isUpstreamResponseModelTerminalEvent(eventType) {
		// A failed/incomplete/cancelled terminal event remains useful for audit,
		// but must never authorize an upstream-declared model for billing.
		o.RejectBilling()
	}
}

func openAIResponseModelEventType(payload []byte, eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType != "" || len(payload) == 0 || !gjson.ValidBytes(payload) {
		return eventType
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(payload, "object").String()), "response") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "status").String())) {
	case "completed", "done":
		return "response.completed"
	case "failed":
		return "response.failed"
	case "incomplete":
		return "response.incomplete"
	case "cancelled":
		return "response.cancelled"
	case "canceled":
		return "response.canceled"
	default:
		return ""
	}
}

func (o *upstreamResponseModelObserver) ObserveAnthropic(payload []byte) {
	model := firstValidTrimmedGJSONModel(payload, "message.model", "model")
	o.Observe(model, false)
	if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(payload, "type").String()), "message_stop") {
		o.MarkProtocolComplete()
	}
}

func (o *upstreamResponseModelObserver) ObserveGemini(payload []byte) {
	model := firstValidTrimmedGJSONModel(
		payload,
		"modelVersion",
		"response.modelVersion",
		"response.response.modelVersion",
	)
	// Gemini streaming has no universal terminal event carrying modelVersion;
	// treating each declaration as terminal retains the latest chunk.
	o.Observe(model, true)
	if geminiResponseHasFinishReason(payload) {
		o.MarkProtocolComplete()
	}
}

func geminiResponseHasFinishReason(payload []byte) bool {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return false
	}
	for _, path := range []string{
		"candidates.0.finishReason",
		"response.candidates.0.finishReason",
		"response.response.candidates.0.finishReason",
	} {
		value := gjson.GetBytes(payload, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return true
		}
	}
	return false
}

func (o *upstreamResponseModelObserver) Model() string {
	if o == nil {
		return ""
	}
	if o.terminal != "" {
		return o.terminal
	}
	return o.first
}

func (o *upstreamResponseModelObserver) Conflict() bool {
	return o != nil && o.conflict
}

func (o *upstreamResponseModelObserver) BillingEligible() bool {
	return o != nil && o.billingEligible
}

// MarkProtocolComplete records an explicit upstream protocol boundary. It is
// intentionally separate from billing admission: parsers may preserve a
// partial response for compatibility, but a bare EOF must not authorize the
// observed model as a billing basis.
func (o *upstreamResponseModelObserver) MarkProtocolComplete() {
	if o != nil {
		o.protocolComplete = true
	}
}

func (o *upstreamResponseModelObserver) ProtocolComplete() bool {
	return o != nil && o.protocolComplete
}

// MarkBillingEligible authorizes the observed declaration only after the
// caller has established that the complete upstream request (or WS turn)
// succeeded. Observation and billing admission intentionally remain separate:
// partial/failed responses still need audit data but must use baseline billing.
func (o *upstreamResponseModelObserver) MarkBillingEligible() {
	if o != nil && !o.billingRejected && o.Model() != "" {
		o.billingEligible = true
	}
}

// RejectBilling is sticky for the lifetime of one attempt/turn. This prevents
// a generic successful HTTP return path from re-authorizing a declaration after
// an explicit failed/incomplete/cancelled protocol terminal event was observed.
func (o *upstreamResponseModelObserver) RejectBilling() {
	if o != nil {
		o.billingEligible = false
		o.billingRejected = true
	}
}

func beginUpstreamResponseModelObservation(c *gin.Context) *upstreamResponseModelObserver {
	observer := &upstreamResponseModelObserver{}
	if c != nil {
		c.Set(upstreamResponseModelObserverContextKey, observer)
	}
	return observer
}

func upstreamResponseModelObserverFromContext(c *gin.Context) *upstreamResponseModelObserver {
	if c == nil {
		return nil
	}
	value, ok := c.Get(upstreamResponseModelObserverContextKey)
	if !ok {
		return nil
	}
	observer, _ := value.(*upstreamResponseModelObserver)
	return observer
}

func observedUpstreamResponseModel(c *gin.Context) string {
	return upstreamResponseModelObserverFromContext(c).Model()
}

func observedUpstreamResponseModelConflict(c *gin.Context) bool {
	return upstreamResponseModelObserverFromContext(c).Conflict()
}

func observedUpstreamResponseModelBillingEligible(c *gin.Context) bool {
	return upstreamResponseModelObserverFromContext(c).BillingEligible()
}

func observedUpstreamResponseModelProtocolComplete(c *gin.Context) bool {
	return upstreamResponseModelObserverFromContext(c).ProtocolComplete()
}

func markObservedUpstreamResponseModelProtocolComplete(c *gin.Context) {
	upstreamResponseModelObserverFromContext(c).MarkProtocolComplete()
}

func markObservedUpstreamResponseModelBillingEligible(c *gin.Context) {
	upstreamResponseModelObserverFromContext(c).MarkBillingEligible()
}

func applyObservedUpstreamResponseModelToForwardResult(c *gin.Context, result *ForwardResult, successful bool) *ForwardResult {
	if result == nil {
		return nil
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if successful {
		observer.MarkBillingEligible()
	}
	result.UpstreamResponseModel = observer.Model()
	result.UpstreamResponseModelConflict = observer.Conflict()
	result.UpstreamResponseModelBillingEligible = observer.BillingEligible()
	return result
}

func applyUpstreamResponseModelToOpenAIForwardResult(result *OpenAIForwardResult, observer *upstreamResponseModelObserver, successful bool) *OpenAIForwardResult {
	if result == nil {
		return nil
	}
	if successful {
		observer.MarkBillingEligible()
	}
	result.UpstreamResponseModel = observer.Model()
	result.UpstreamResponseModelConflict = observer.Conflict()
	result.UpstreamResponseModelBillingEligible = observer.BillingEligible()
	return result
}

func applyObservedUpstreamResponseModelToOpenAIForwardResult(c *gin.Context, result *OpenAIForwardResult, successful bool) *OpenAIForwardResult {
	return applyUpstreamResponseModelToOpenAIForwardResult(result, upstreamResponseModelObserverFromContext(c), successful)
}

func observeOpenAISSEBody(observer *upstreamResponseModelObserver, body string) {
	if observer == nil || strings.TrimSpace(body) == "" {
		return
	}
	forEachOpenAISSEDataPayload(body, func(payload []byte) {
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		observer.ObserveOpenAI(payload, eventType)
	})
}

func firstValidTrimmedGJSONModel(payload []byte, paths ...string) string {
	if len(payload) == 0 {
		return ""
	}
	for _, path := range paths {
		value := gjson.GetBytes(payload, path)
		if !value.Exists() || value.Type != gjson.String {
			continue
		}
		if model := strings.TrimSpace(value.String()); model != "" {
			// Validate only after finding a candidate. This avoids a full validation
			// pass on the common model-free delta path while still rejecting malformed
			// payloads that appear to declare a model.
			if !gjson.ValidBytes(payload) {
				return ""
			}
			return model
		}
	}
	return ""
}

func isUpstreamResponseModelTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func isUpstreamResponseModelSuccessfulTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done":
		return true
	default:
		return false
	}
}

func upstreamModelMismatch(sentModel, responseModel string) *bool {
	responseModel = strings.TrimSpace(responseModel)
	if responseModel == "" {
		return nil
	}
	sentModel = strings.TrimSpace(sentModel)
	mismatch := sentModel == "" || !strings.EqualFold(sentModel, responseModel)
	return &mismatch
}

func upstreamSentModel(requestedModel, upstreamModel string) string {
	sentModel := strings.TrimSpace(upstreamModel)
	if sentModel == "" {
		sentModel = strings.TrimSpace(requestedModel)
	}
	return sentModel
}
