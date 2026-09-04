package service

import "context"

// IsAccountShareModeGroup reports whether groupID belongs to account-share mode.
// Repository failures are deliberately returned to the handler: treating a failed
// lookup as "not a mode group" would allow a request to cross the routing boundary.
func (s *GatewayService) IsAccountShareModeGroup(ctx context.Context, groupID int64) (bool, error) {
	if s == nil || s.accountShareModeService == nil || groupID <= 0 {
		return false, nil
	}
	return s.accountShareModeService.IsModeGroupChecked(ctx, groupID)
}

// IsAccountShareModeGroup is the OpenAI gateway counterpart of the generic
// gateway classifier. Keep both entry points identical so every protocol uses
// the same fail-closed route-isolation rule.
func (s *OpenAIGatewayService) IsAccountShareModeGroup(ctx context.Context, groupID int64) (bool, error) {
	if s == nil || s.accountShareModeService == nil || groupID <= 0 {
		return false, nil
	}
	return s.accountShareModeService.IsModeGroupChecked(ctx, groupID)
}
