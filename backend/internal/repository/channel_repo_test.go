//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// --- marshalModelMapping ---

func TestMarshalModelMapping(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]map[string]string
		wantJSON string // expected JSON output (exact match)
	}{
		{
			name:     "empty map",
			input:    map[string]map[string]string{},
			wantJSON: "{}",
		},
		{
			name:     "nil map",
			input:    nil,
			wantJSON: "{}",
		},
		{
			name: "populated map",
			input: map[string]map[string]string{
				"openai": {"gpt-4": "gpt-4-turbo"},
			},
		},
		{
			name: "nested values",
			input: map[string]map[string]string{
				"openai":    {"*": "gpt-5.4"},
				"anthropic": {"claude-old": "claude-new"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalModelMapping(tt.input)
			require.NoError(t, err)

			if tt.wantJSON != "" {
				require.Equal(t, []byte(tt.wantJSON), result)
			} else {
				// round-trip: unmarshal and compare with input
				var parsed map[string]map[string]string
				require.NoError(t, json.Unmarshal(result, &parsed))
				require.Equal(t, tt.input, parsed)
			}
		})
	}
}

// --- unmarshalModelMapping ---

func TestUnmarshalModelMapping(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantNil bool
		want    map[string]map[string]string
	}{
		{
			name:    "nil data",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "empty data",
			input:   []byte{},
			wantNil: true,
		},
		{
			name:    "invalid JSON",
			input:   []byte("not-json"),
			wantNil: true,
		},
		{
			name:    "type error - number",
			input:   []byte("42"),
			wantNil: true,
		},
		{
			name:    "type error - array",
			input:   []byte("[1,2,3]"),
			wantNil: true,
		},
		{
			name:  "valid JSON",
			input: []byte(`{"openai":{"gpt-4":"gpt-4-turbo"},"anthropic":{"old":"new"}}`),
			want: map[string]map[string]string{
				"openai":    {"gpt-4": "gpt-4-turbo"},
				"anthropic": {"old": "new"},
			},
		},
		{
			name:  "empty object",
			input: []byte("{}"),
			want:  map[string]map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unmarshalModelMapping(tt.input)
			if tt.wantNil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.want, result)
			}
		})
	}
}

// --- escapeLike ---

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no special chars",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "backslash",
			input: `a\b`,
			want:  `a\\b`,
		},
		{
			name:  "percent",
			input: "50%",
			want:  `50\%`,
		},
		{
			name:  "underscore",
			input: "a_b",
			want:  `a\_b`,
		},
		{
			name:  "all special chars",
			input: `a\b%c_d`,
			want:  `a\\b\%c\_d`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "consecutive special chars",
			input: "%_%",
			want:  `\%\_\%`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, escapeLike(tt.input))
		})
	}
}

// --- isUniqueViolation ---

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unique violation code 23505",
			err:  &pq.Error{Code: "23505"},
			want: true,
		},
		{
			name: "different pq error code",
			err:  &pq.Error{Code: "23503"},
			want: false,
		},
		{
			name: "non-pq error",
			err:  errors.New("some generic error"),
			want: false,
		},
		{
			name: "typed nil pq.Error",
			err: func() error {
				var pqErr *pq.Error
				return pqErr
			}(),
			want: false,
		},
		{
			name: "bare nil",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped pq error with 23505",
			err:  fmt.Errorf("wrapped: %w", &pq.Error{Code: "23505"}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isUniqueViolation(tt.err))
		})
	}
}

func TestChannelListOrderBy_AllowsDescendingIDSort(t *testing.T) {
	params := pagination.PaginationParams{
		SortBy:    "id",
		SortOrder: "desc",
	}

	require.Equal(t, "c.id DESC, c.id DESC", channelListOrderBy(params))
}

func TestScanModelPricingRows_LongContextPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "channel_id", "platform", "models", "billing_mode",
		"input_price", "output_price", "cache_write_price", "cache_read_price",
		"image_input_price", "image_cache_read_price", "image_output_price", "per_request_price",
		"long_context_pricing_enabled", "long_context_input_token_threshold", "created_at", "updated_at",
	}).AddRow(
		int64(11), int64(7), service.PlatformOpenAI, []byte(`["gpt-5.6-sol"]`), service.BillingModeToken,
		nil, nil, nil, nil, nil, nil, nil, nil, true, 128000, now, now,
	).AddRow(
		int64(12), int64(7), service.PlatformOpenAI, []byte(`["gpt-5.6-terra"]`), service.BillingModeToken,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, now, now,
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	sqlRows, err := db.QueryContext(context.Background(), "SELECT")
	require.NoError(t, err)
	defer func() { require.NoError(t, sqlRows.Close()) }()

	pricing, ids, err := scanModelPricingRows(sqlRows)
	require.NoError(t, err)
	require.Equal(t, []int64{11, 12}, ids)
	require.Len(t, pricing, 2)
	require.NotNil(t, pricing[0].LongContextPricingEnabled)
	require.True(t, *pricing[0].LongContextPricingEnabled)
	require.Equal(t, 128000, *pricing[0].LongContextInputTokenThreshold)
	require.Nil(t, pricing[1].LongContextPricingEnabled)
	require.Nil(t, pricing[1].LongContextInputTokenThreshold)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateModelPricingExec_PersistsLongContextPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	enabled := true
	threshold := 128000
	now := time.Now().UTC()
	pricing := &service.ChannelModelPricing{
		ChannelID:                      7,
		Platform:                       service.PlatformOpenAI,
		Models:                         []string{"gpt-5.6-sol"},
		BillingMode:                    service.BillingModeToken,
		LongContextPricingEnabled:      &enabled,
		LongContextInputTokenThreshold: &threshold,
	}
	mock.ExpectQuery("INSERT INTO channel_model_pricing").
		WithArgs(
			int64(7), service.PlatformOpenAI, []byte(`["gpt-5.6-sol"]`), service.BillingModeToken,
			nil, nil, nil, nil, nil, nil, nil, nil, true, int64(128000),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), now, now))

	require.NoError(t, createModelPricingExec(context.Background(), db, pricing))
	require.Equal(t, int64(11), pricing.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
