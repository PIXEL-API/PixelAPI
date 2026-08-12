package service

import (
	"log/slog"
	"strings"
)

const responseModelBillingCostEpsilon = 1e-12

// responseModelBillingDeclaration returns a response-declared model only for
// an explicit opt-in with an unambiguous, token-only response. New non-token
// billing modes must be included by callers through nonTokenBilled.
func responseModelBillingDeclaration(source, responseModel string, conflict, nonTokenBilled, billingEligible bool) string {
	if source != BillingModelSourceResponse || conflict || nonTokenBilled || !billingEligible {
		return ""
	}
	return strings.TrimSpace(responseModel)
}

// responseModelBillingAdoptable enforces the safety invariants for replacing a
// baseline cost with one calculated from an untrusted upstream declaration.
func responseModelBillingAdoptable(baseline, response *CostBreakdown, baselineChannelPriced, responseChannelPriced bool) bool {
	if baseline == nil || response == nil {
		return false
	}
	// An upstream declaration must never increase the user's charge.
	if response.TotalCost > baseline.TotalCost+responseModelBillingCostEpsilon {
		return false
	}
	// A declared zero-price model must not erase an otherwise billable request.
	if response.TotalCost <= 0 && baseline.TotalCost > 0 {
		return false
	}
	// An administrator's explicit channel price may only be replaced by another
	// explicit channel price; otherwise a dated alias could bypass that policy.
	return !baselineChannelPriced || responseChannelPriced
}

func logResponseModelBillingApplied(component string, account *Account, requestID, baselineModel, responseModel string, baselineCost, responseCost *CostBreakdown) {
	baselineModel = strings.TrimSpace(baselineModel)
	responseModel = strings.TrimSpace(responseModel)
	if strings.EqualFold(baselineModel, responseModel) {
		return
	}
	attrs := []any{
		"component", component,
		"request_id", strings.TrimSpace(requestID),
		"baseline_model", baselineModel,
		"response_model", responseModel,
	}
	if baselineCost != nil && responseCost != nil {
		attrs = append(attrs, "baseline_cost", baselineCost.TotalCost, "billed_cost", responseCost.TotalCost)
	}
	if account != nil {
		attrs = append(attrs, "platform", account.Platform, "account_id", account.ID)
	}
	slog.Info("billing.response_model_applied", attrs...)
}
