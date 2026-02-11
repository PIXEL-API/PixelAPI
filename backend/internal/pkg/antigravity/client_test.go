package antigravity

import (
	"testing"
)

func TestExtractProjectIDFromOnboardResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp map[string]any
		want string
	}{
		{
			name: "nil response",
			resp: nil,
			want: "",
		},
		{
			name: "empty response",
			resp: map[string]any{},
			want: "",
		},
		{
			name: "project as string",
			resp: map[string]any{
				"cloudaicompanionProject": "my-project-123",
			},
			want: "my-project-123",
		},
		{
			name: "project as string with spaces",
			resp: map[string]any{
				"cloudaicompanionProject": "  my-project-123  ",
			},
			want: "my-project-123",
		},
		{
			name: "project as map with id",
			resp: map[string]any{
				"cloudaicompanionProject": map[string]any{
					"id": "proj-from-map",
				},
			},
			want: "proj-from-map",
		},
		{
			name: "project as map without id",
			resp: map[string]any{
				"cloudaicompanionProject": map[string]any{
					"name": "some-name",
				},
			},
			want: "",
		},
		{
			name: "missing cloudaicompanionProject key",
			resp: map[string]any{
				"otherField": "value",
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := extractProjectIDFromOnboardResponse(tc.resp)
			if got != tc.want {
				t.Fatalf("extractProjectIDFromOnboardResponse() = %q, want %q", got, tc.want)
			}
		})
	}
}
