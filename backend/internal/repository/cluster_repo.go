package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type clusterRepository struct {
	db *sql.DB
}

var _ service.ClusterAdminRepository = (*clusterRepository)(nil)

type clusterRowScanner interface {
	Scan(dest ...any) error
}

type clusterQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewClusterRepository(db *sql.DB) service.ClusterAdminRepository {
	return &clusterRepository{db: db}
}

func ProvideClusterRuntimeRepository(repository service.ClusterAdminRepository) service.ClusterRepository {
	return repository
}

func (r *clusterRepository) validate() error {
	if r == nil || r.db == nil {
		return errors.New("nil cluster repository")
	}
	return nil
}

func validateClusterRequired(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func clusterDurationSeconds(name string, duration time.Duration) (int64, error) {
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	seconds := int64((duration + time.Second - 1) / time.Second)
	return seconds, nil
}

func validateClusterState(value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("invalid cluster state %q", value)
}

func clusterNullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func clusterQueryOne[T any](
	ctx context.Context,
	queryer clusterQueryRower,
	query string,
	args []any,
	scan func(clusterRowScanner) (*T, error),
) (*T, error) {
	return scan(queryer.QueryRowContext(ctx, query, args...))
}
