package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateServiceCustomizedDeploymentDisablesInPlaceMutation(t *testing.T) {
	svc := &UpdateService{inPlaceAllowed: false}

	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrInPlaceUpdateDisabled)
	require.ErrorIs(t, svc.Rollback(), ErrInPlaceUpdateDisabled)
}
