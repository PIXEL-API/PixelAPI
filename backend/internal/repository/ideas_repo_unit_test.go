//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIdeasRepositoryListMineFiltersStatusBeforeCountAndPagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = db.Close()
		}
	})

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM idea_posts p WHERE p\.deleted_at IS NULL AND p\.author_user_id = \$1 AND \(\$2 = '' OR p\.status = \$2\)`).
		WithArgs(int64(7), service.IdeaPostStatusDraft).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)SELECT .* FROM idea_posts p .* WHERE p\.deleted_at IS NULL AND p\.author_user_id = \$1 AND \(\$2 = '' OR p\.status = \$2\) .* LIMIT \$3 OFFSET \$4`).
		WithArgs(int64(7), service.IdeaPostStatusDraft, 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"unused"}))
	mock.ExpectClose()

	repo := NewIdeasRepository(db)
	items, total, err := repo.ListMine(context.Background(), 7, service.IdeaPostListParams{
		Page:     2,
		PageSize: 20,
		Status:   service.IdeaPostStatusDraft,
	})
	require.NoError(t, err)
	require.Empty(t, items)
	require.Zero(t, total)
	require.NoError(t, db.Close())
	closed = true
	require.NoError(t, mock.ExpectationsWereMet())
}
