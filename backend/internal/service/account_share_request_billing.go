package service

import "context"

// IsAccountShareModeBillingRequest distinguishes a resolved room request from
// the routing marker that is also attached to ordinary API key requests.
func IsAccountShareModeBillingRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if lease, _ := ctx.Value(accountShareRuntimeLeaseContextKey{}).(*AccountShareRuntimeLease); lease != nil {
		return true
	}
	request, ok := AccountShareModeRequestFromContext(ctx)
	if !ok || request.state == nil {
		return false
	}
	request.state.mu.RLock()
	defer request.state.mu.RUnlock()
	return request.state.resolved && request.state.membership != nil
}
