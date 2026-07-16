package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/redis/go-redis/v9"
)

func TestServerTimingRedisHookRecordsCountsWithoutCommandDetails(t *testing.T) {
	collector := servertiming.New(time.Now())
	ctx := servertiming.WithCollector(context.Background(), collector)
	hook := serverTimingRedisHook{}
	wantErr := errors.New("redis unavailable")
	process := hook.ProcessHook(func(gotCtx context.Context, cmd redis.Cmder) error {
		if gotCtx != ctx || cmd.Name() != "get" {
			t.Fatalf("hook changed Redis call semantics")
		}
		return wantErr
	})
	cmd := redis.NewStringCmd(ctx, "get", "secret:key")
	if err := process(ctx, cmd); !errors.Is(err, wantErr) {
		t.Fatalf("ProcessHook() error = %v, want %v", err, wantErr)
	}

	pipeline := hook.ProcessPipelineHook(func(gotCtx context.Context, cmds []redis.Cmder) error {
		if gotCtx != ctx || len(cmds) != 2 {
			t.Fatalf("pipeline hook changed Redis call semantics")
		}
		return nil
	})
	if err := pipeline(ctx, []redis.Cmder{
		redis.NewStringCmd(ctx, "get", "token:secret"),
		redis.NewStatusCmd(ctx, "set", "credential", "value"),
	}); err != nil {
		t.Fatal(err)
	}

	header := collector.HeaderValue(time.Now(), "bypass")
	if !strings.Contains(header, `commands=3`) {
		t.Fatalf("Redis count missing: %q", header)
	}
	for _, forbidden := range []string{"secret:key", "token:secret", "credential", "value", "get", "set"} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("Redis detail %q leaked into %q", forbidden, header)
		}
	}
}

func TestServerTimingRedisHookInactiveFastPath(t *testing.T) {
	ctx := context.Background()
	called := false
	hook := serverTimingRedisHook{}
	process := hook.ProcessHook(func(gotCtx context.Context, _ redis.Cmder) error {
		called = true
		if gotCtx != ctx {
			t.Fatal("inactive hook changed context")
		}
		return nil
	})
	if err := process(ctx, redis.NewStringCmd(ctx, "ping")); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("inactive command did not reach next hook")
	}
}
