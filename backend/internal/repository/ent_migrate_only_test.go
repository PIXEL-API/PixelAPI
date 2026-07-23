package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestApplyConfiguredMigrationsRejectsNilInputs(t *testing.T) {
	var nilContext context.Context

	tests := []struct {
		name    string
		ctx     context.Context
		cfg     *config.Config
		wantErr string
	}{
		{name: "nil context", ctx: nilContext, cfg: &config.Config{}, wantErr: "nil migration context"},
		{name: "nil config", ctx: context.Background(), wantErr: "nil config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ApplyConfiguredMigrations(tt.ctx, tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ApplyConfiguredMigrations() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
