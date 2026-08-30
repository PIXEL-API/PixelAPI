//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIdeasRepositoryPublishRevision(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{Username: "ideas-publish-author"})
	repo := NewIdeasRepository(integrationDB)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_audit_events
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_tags
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_revisions
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM idea_posts WHERE author_user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	post, err := repo.CreatePostWithRevision(ctx, service.IdeaPostCreateInput{
		AuthorUserID: user.ID,
		Title:        "发布锁回归测试",
		Summary:      "验证带有作者外连接的文章查询只锁定文章主行",
		Body:         "正文",
	}, nil)
	require.NoError(t, err)

	published, err := repo.PublishRevision(ctx, post.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, service.IdeaPostStatusPendingReview, published.Status)
	require.NotNil(t, published.Revision)
	require.Equal(t, service.IdeaRevisionStatusPendingReview, published.Revision.ModerationStatus)

	var publishAuditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM idea_post_audit_events
		WHERE post_id = $1 AND action = 'publish'`, post.ID).Scan(&publishAuditCount))
	require.Equal(t, 1, publishAuditCount)

	_, err = repo.PublishRevision(ctx, post.ID, user.ID)
	require.ErrorIs(t, err, service.ErrIdeaPostInvalidState)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM idea_post_audit_events
		WHERE post_id = $1 AND action = 'publish'`, post.ID).Scan(&publishAuditCount))
	require.Equal(t, 1, publishAuditCount)
}

func TestIdeasRepositoryListMineFiltersStatusBeforePagination(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{Username: "ideas-list-status-author"})
	repo := NewIdeasRepository(integrationDB)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_audit_events
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_tags
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			DELETE FROM idea_post_revisions
			WHERE post_id IN (SELECT id FROM idea_posts WHERE author_user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM idea_posts WHERE author_user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	create := func(title string) *service.IdeaPost {
		t.Helper()
		post, err := repo.CreatePostWithRevision(ctx, service.IdeaPostCreateInput{
			AuthorUserID: user.ID,
			Title:        title,
			Body:         "正文",
		}, nil)
		require.NoError(t, err)
		return post
	}

	create("草稿一")
	pending := create("待审核")
	_, err := repo.PublishRevision(ctx, pending.ID, user.ID)
	require.NoError(t, err)
	create("草稿二")

	firstPage, total, err := repo.ListMine(ctx, user.ID, service.IdeaPostListParams{
		Page:     1,
		PageSize: 1,
		Status:   service.IdeaPostStatusDraft,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, firstPage, 1)
	require.Equal(t, service.IdeaPostStatusDraft, firstPage[0].Status)

	secondPage, total, err := repo.ListMine(ctx, user.ID, service.IdeaPostListParams{
		Page:     2,
		PageSize: 1,
		Status:   service.IdeaPostStatusDraft,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, secondPage, 1)
	require.Equal(t, service.IdeaPostStatusDraft, secondPage[0].Status)
}
