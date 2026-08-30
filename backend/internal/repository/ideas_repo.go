package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type ideasRepository struct {
	db *sql.DB
}

func NewIdeasRepository(db *sql.DB) service.IdeasRepository {
	return &ideasRepository{db: db}
}

const ideaPostColumns = `
	p.id,
	p.author_user_id,
	COALESCE(u.username, '') AS author_name,
	COALESCE(p.current_revision_id, 0) AS current_revision_id,
	p.status,
	p.published_at,
	p.like_count,
	p.favorite_count,
	p.view_count,
	p.created_at,
	p.updated_at`

const ideaRevisionColumns = `
	r.id,
	r.post_id,
	r.revision_no,
	r.title,
	r.summary,
	r.body,
	r.body_hash,
	r.moderation_status,
	COALESCE(r.moderation_reason, ''),
	r.published_at,
	r.created_by,
	r.created_at`

const ideaTagColumns = `id, name, slug, status, sort_order, usage_count`

func (r *ideasRepository) CreatePostWithRevision(ctx context.Context, input service.IdeaPostCreateInput, tags []service.IdeaTag) (*service.IdeaPost, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	bodyHash := hashBody(input.Body)
	var postID, revisionID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO idea_posts (author_user_id, status)
		VALUES ($1, $2)
		RETURNING id`,
		input.AuthorUserID, service.IdeaPostStatusDraft,
	).Scan(&postID); err != nil {
		return nil, err
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO idea_post_revisions (post_id, revision_no, title, summary, body, body_hash, moderation_status, created_by)
		VALUES ($1, 1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		postID, input.Title, input.Summary, input.Body, bodyHash, service.IdeaRevisionStatusDraft, input.AuthorUserID,
	).Scan(&revisionID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE idea_posts SET current_revision_id = $1 WHERE id = $2`, revisionID, postID); err != nil {
		return nil, err
	}

	if err := replacePostTags(ctx, tx, postID, tags); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_audit_events (post_id, actor_user_id, action, after)
		VALUES ($1, $2, $3, $4::jsonb)`,
		postID, input.AuthorUserID, "create",
		fmt.Sprintf(`{"status": %q}`, service.IdeaPostStatusDraft),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, postID)
}

func (r *ideasRepository) UpdateDraftRevision(ctx context.Context, input service.IdeaPostUpdateInput, tags []service.IdeaTag) (*service.IdeaPost, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	post, err := getIdeaPostForUpdate(ctx, tx, input.PostID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != input.UserID {
		return nil, service.ErrIdeaPostNotAuthor
	}
	if post.Status != service.IdeaPostStatusDraft && post.Status != service.IdeaPostStatusRejected {
		return nil, service.ErrIdeaPostInvalidState
	}

	bodyHash := hashBody(input.Body)
	res, err := tx.ExecContext(ctx, `
		UPDATE idea_post_revisions
		SET title = $1, summary = $2, body = $3, body_hash = $4, created_by = $5
		WHERE id = $6 AND moderation_status IN ($7, $8)`,
		input.Title, input.Summary, input.Body, bodyHash, input.UserID, post.CurrentRevisionID,
		service.IdeaRevisionStatusDraft, service.IdeaRevisionStatusRejected,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, service.ErrIdeaPostInvalidState
	}

	if err := replacePostTags(ctx, tx, input.PostID, tags); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, input.PostID)
}

func (r *ideasRepository) PublishRevision(ctx context.Context, postID, userID int64) (*service.IdeaPost, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	post, err := getIdeaPostForUpdate(ctx, tx, postID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != userID {
		return nil, service.ErrIdeaPostNotAuthor
	}
	if post.Status != service.IdeaPostStatusDraft && post.Status != service.IdeaPostStatusRejected {
		return nil, service.ErrIdeaPostInvalidState
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_post_revisions
		SET moderation_status = $1
		WHERE id = (
			SELECT id FROM idea_post_revisions WHERE post_id = $2 ORDER BY revision_no DESC LIMIT 1
		)`,
		service.IdeaRevisionStatusPendingReview, postID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_posts SET status = $1, updated_at = NOW() WHERE id = $2`,
		service.IdeaPostStatusPendingReview, postID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_audit_events (post_id, actor_user_id, action, after)
		VALUES ($1, $2, $3, $4::jsonb)`,
		postID, userID, "publish", fmt.Sprintf(`{"status": %q}`, service.IdeaPostStatusPendingReview),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, postID)
}

func (r *ideasRepository) EditPublishedRevision(ctx context.Context, input service.IdeaPostUpdateInput, tags []service.IdeaTag) (*service.IdeaPost, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	post, err := getIdeaPostForUpdate(ctx, tx, input.PostID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != input.UserID {
		return nil, service.ErrIdeaPostNotAuthor
	}
	if post.Status != service.IdeaPostStatusPublished {
		return nil, service.ErrIdeaPostInvalidState
	}

	var nextNo int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(revision_no), 0) + 1 FROM idea_post_revisions WHERE post_id = $1`,
		input.PostID,
	).Scan(&nextNo); err != nil {
		return nil, err
	}

	bodyHash := hashBody(input.Body)
	var revisionID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO idea_post_revisions (post_id, revision_no, title, summary, body, body_hash, moderation_status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		input.PostID, nextNo, input.Title, input.Summary, input.Body, bodyHash, service.IdeaRevisionStatusPendingRevision, input.UserID,
	).Scan(&revisionID); err != nil {
		return nil, err
	}

	// 保持 current_revision_id 指向旧公开版本；新版本进入待审核，审核期间公开内容不变。
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_posts SET status = $1, updated_at = NOW() WHERE id = $2`,
		service.IdeaPostStatusPendingRevision, input.PostID,
	); err != nil {
		return nil, err
	}

	if err := replacePostTags(ctx, tx, input.PostID, tags); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_audit_events (post_id, actor_user_id, action, after)
		VALUES ($1, $2, $3, $4::jsonb)`,
		input.PostID, input.UserID, "edit", fmt.Sprintf(`{"status": %q, "revision_no": %d}`, service.IdeaPostStatusPendingRevision, nextNo),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, input.PostID)
}

func (r *ideasRepository) SoftDeletePost(ctx context.Context, postID, userID int64) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollbackUnlessDone(tx)

	post, err := getIdeaPostForUpdate(ctx, tx, postID)
	if err != nil {
		return err
	}
	if post.AuthorUserID != userID {
		return service.ErrIdeaPostNotAuthor
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_posts SET status = $1, deleted_at = NOW(), updated_at = NOW() WHERE id = $2`,
		service.IdeaPostStatusDeleted, postID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_audit_events (post_id, actor_user_id, action, after)
		VALUES ($1, $2, $3, $4::jsonb)`,
		postID, userID, "delete", fmt.Sprintf(`{"status": %q}`, service.IdeaPostStatusDeleted),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// GetPost 返回「作者/管理员工作视图」：Revision 取最新版本。
func (r *ideasRepository) GetPost(ctx context.Context, postID int64) (*service.IdeaPost, error) {
	return r.getPost(ctx, postID, true)
}

// GetPublicPost 返回「公开视图」：Revision 采用 current_revision_id（已批准版本），
// 用于用户公开阅读，避免展示未经审核或被拒的最新修订。
func (r *ideasRepository) GetPublicPost(ctx context.Context, postID int64) (*service.IdeaPost, error) {
	return r.getPost(ctx, postID, false)
}

// getPost 按 revisionMode 决定关联哪个版本：latest（工作视图）或 current（公开视图）。
func (r *ideasRepository) getPost(ctx context.Context, postID int64, useLatest bool) (*service.IdeaPost, error) {
	joinClause := `JOIN idea_post_revisions r ON r.id = p.current_revision_id`
	if useLatest {
		joinClause = `JOIN idea_post_revisions r
			ON r.post_id = p.id
			AND r.revision_no = (SELECT MAX(revision_no) FROM idea_post_revisions WHERE post_id = p.id)`
	}
	query := fmt.Sprintf(`
		SELECT %s, %s
		FROM idea_posts p
		LEFT JOIN users u ON u.id = p.author_user_id
		%s
		WHERE p.id = $1 AND p.deleted_at IS NULL`, ideaPostColumns, ideaRevisionColumns, joinClause)

	post, err := scanIdeaPost(r.db.QueryRowContext(ctx, query, postID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrIdeaPostNotFound
		}
		return nil, err
	}
	tags, err := r.loadTags(ctx, postID)
	if err != nil {
		return nil, err
	}
	post.Tags = tags
	return post, nil
}

func (r *ideasRepository) ListPublished(ctx context.Context, params service.IdeaPostListParams) ([]service.IdeaPost, int64, error) {
	where := `p.deleted_at IS NULL AND p.status = 'published'`
	args := []any{}
	return r.list(ctx, where, args, params, false)
}

func (r *ideasRepository) ListMine(ctx context.Context, userID int64, params service.IdeaPostListParams) ([]service.IdeaPost, int64, error) {
	where := `p.deleted_at IS NULL AND p.author_user_id = $1 AND ($2 = '' OR p.status = $2)`
	args := []any{userID, params.Status}
	return r.list(ctx, where, args, params, true)
}

func (r *ideasRepository) ListAdmin(ctx context.Context, params service.IdeaPostListParams) ([]service.IdeaPost, int64, error) {
	where := `p.deleted_at IS NULL`
	args := []any{}
	return r.list(ctx, where, args, params, true)
}

func (r *ideasRepository) list(ctx context.Context, where string, args []any, params service.IdeaPostListParams, useLatest bool) ([]service.IdeaPost, int64, error) {
	page, pageSize := normalizeIdeaPagination(params.Page, params.PageSize)

	// 标签筛选
	if slug := strings.TrimSpace(params.TagSlug); slug != "" {
		args = append(args, slug)
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM idea_post_tags pt JOIN idea_tags t ON t.id = pt.tag_id
			WHERE pt.post_id = p.id AND t.slug = $%d AND t.status = 'active')`, len(args))
	}
	// 关键词搜索（标题/摘要）
	if kw := strings.TrimSpace(params.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		where += fmt.Sprintf(` AND p.id IN (
			SELECT r.post_id FROM idea_post_revisions r
			WHERE r.title ILIKE $%d OR r.summary ILIKE $%d)`, len(args), len(args))
	}

	countSQL := `SELECT COUNT(*) FROM idea_posts p WHERE ` + where
	var total int64
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	joinClause := `JOIN idea_post_revisions r ON r.id = p.current_revision_id`
	if useLatest {
		joinClause = `JOIN idea_post_revisions r
			ON r.post_id = p.id
			AND r.revision_no = (SELECT MAX(revision_no) FROM idea_post_revisions WHERE post_id = p.id)`
	}

	orderBy := "p.published_at DESC, p.id DESC"
	if params.Sort == "hot" {
		orderBy = "(p.like_count * 3 + p.favorite_count * 2 + p.view_count) DESC, p.published_at DESC"
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT %s, %s
		FROM idea_posts p
		LEFT JOIN users u ON u.id = p.author_user_id
		%s
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, ideaPostColumns, ideaRevisionColumns, joinClause, where, orderBy, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.IdeaPost, 0)
	for rows.Next() {
		post, err := scanIdeaPost(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *post)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(items) > 0 {
		if err := r.attachTags(ctx, items); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (r *ideasRepository) SetPostStatus(ctx context.Context, postID int64, status, revisionStatus, reason string, operatorUserID int64, isApprove bool) (*service.IdeaPost, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	post, err := getIdeaPostForUpdate(ctx, tx, postID)
	if err != nil {
		return nil, err
	}
	if post.Status == service.IdeaPostStatusDeleted {
		return nil, service.ErrIdeaPostNotFound
	}

	// 审核操作统一作用于最新版本（工作版本）。
	finalStatus := status
	if revisionStatus != "" {
		var targetRevisionID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM idea_post_revisions WHERE post_id = $1 ORDER BY revision_no DESC LIMIT 1`,
			postID).Scan(&targetRevisionID); err != nil {
			return nil, err
		}

		if isApprove {
			if _, err := tx.ExecContext(ctx, `
				UPDATE idea_post_revisions
				SET moderation_status = $1, moderation_reason = NULL, published_at = NOW()
				WHERE id = $2`,
				revisionStatus, targetRevisionID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE idea_posts
				SET status = $1, current_revision_id = $2,
					published_at = COALESCE(published_at, NOW()), updated_at = NOW()
				WHERE id = $3`,
				status, targetRevisionID, postID); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE idea_post_revisions
				SET moderation_status = $1, moderation_reason = $2
				WHERE id = $3`,
				revisionStatus, reason, targetRevisionID); err != nil {
				return nil, err
			}
			// 拒绝已发布文章的修订时，保留旧公开版本继续可见
			if post.Status == service.IdeaPostStatusPendingRevision {
				finalStatus = service.IdeaPostStatusPublished
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE idea_posts SET status = $1, updated_at = NOW() WHERE id = $2`,
				finalStatus, postID); err != nil {
				return nil, err
			}
		}
	} else {
		// 无版本状态变更（hide / restore）
		if _, err := tx.ExecContext(ctx, `
			UPDATE idea_posts SET status = $1, updated_at = NOW() WHERE id = $2`,
			finalStatus, postID); err != nil {
			return nil, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_audit_events (post_id, actor_user_id, action, before, after, reason)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)`,
		postID, operatorUserID, "moderate",
		fmt.Sprintf(`{"status": %q}`, post.Status),
		fmt.Sprintf(`{"status": %q}`, finalStatus),
		nullIfEmpty(reason),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, postID)
}

func (r *ideasRepository) ListTags(ctx context.Context) ([]service.IdeaTag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ideaTagColumns+`
		FROM idea_tags
		WHERE status = 'active'
		ORDER BY sort_order DESC, usage_count DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := make([]service.IdeaTag, 0)
	for rows.Next() {
		var t service.IdeaTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (r *ideasRepository) ResolveTags(ctx context.Context, slugs []string) ([]service.IdeaTag, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	out := make([]service.IdeaTag, 0, len(slugs))
	for _, raw := range slugs {
		slug := slugifyIdeaTag(raw)
		if slug == "" {
			return nil, service.ErrIdeaTagNameInvalid
		}
		name := strings.TrimSpace(raw)

		var t service.IdeaTag
		var redirectSlug string
		err := tx.QueryRowContext(ctx, `
			SELECT `+ideaTagColumns+`, COALESCE(redirect_to_slug, '') FROM idea_tags WHERE slug = $1
			FOR UPDATE`, slug).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount, &redirectSlug)
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO idea_tags (name, slug)
				VALUES ($1, $2)
				ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
				RETURNING `+ideaTagColumns,
				name, slug,
			).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else if redirectSlug != "" {
			// 命中合并后的旧 slug：跟随重定向解析到目标标签。
			if err := tx.QueryRowContext(ctx, `
				SELECT `+ideaTagColumns+` FROM idea_tags WHERE slug = $1
				FOR UPDATE`, redirectSlug).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, service.ErrIdeaTagNotFound
				}
				return nil, err
			}
		}
		out = append(out, t)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func replacePostTags(ctx context.Context, tx *sql.Tx, postID int64, tags []service.IdeaTag) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM idea_post_tags WHERE post_id = $1`, postID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idea_post_tags (post_id, tag_id) VALUES ($1, $2)
			ON CONFLICT (post_id, tag_id) DO NOTHING`, postID, t.ID); err != nil {
			return err
		}
	}
	// 更新标签使用计数
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_tags t
		SET usage_count = (SELECT COUNT(*) FROM idea_post_tags pt WHERE pt.tag_id = t.id),
			updated_at = NOW()
		WHERE t.id = ANY($1)`,
		pq.Array(tagIDs(tags)),
	); err != nil {
		return err
	}
	return nil
}

func (r *ideasRepository) loadTags(ctx context.Context, postID int64) ([]service.IdeaTag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ideaTagColumns+`
		FROM idea_tags t
		JOIN idea_post_tags pt ON pt.tag_id = t.id
		WHERE pt.post_id = $1
		ORDER BY t.sort_order DESC, t.id ASC`, postID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := make([]service.IdeaTag, 0)
	for rows.Next() {
		var t service.IdeaTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (r *ideasRepository) attachTags(ctx context.Context, items []service.IdeaPost) error {
	ids := make([]int64, 0, len(items))
	for _, p := range items {
		ids = append(ids, p.ID)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT pt.post_id, `+ideaTagColumns+`
		FROM idea_post_tags pt
		JOIN idea_tags t ON t.id = pt.tag_id
		WHERE pt.post_id = ANY($1)
		ORDER BY t.sort_order DESC, t.id ASC`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	tagMap := make(map[int64][]service.IdeaTag, len(items))
	for rows.Next() {
		var postID int64
		var t service.IdeaTag
		if err := rows.Scan(&postID, &t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
			return err
		}
		tagMap[postID] = append(tagMap[postID], t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		items[i].Tags = tagMap[items[i].ID]
	}
	return nil
}

func getIdeaPostForUpdate(ctx context.Context, tx *sql.Tx, postID int64) (*service.IdeaPost, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM idea_posts p
		LEFT JOIN users u ON u.id = p.author_user_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
		FOR UPDATE OF p`, ideaPostColumns)
	post, err := scanIdeaPostBase(tx.QueryRowContext(ctx, query, postID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrIdeaPostNotFound
	}
	return post, err
}

func scanIdeaPost(row ideaRowScanner) (*service.IdeaPost, error) {
	var post service.IdeaPost
	post.Revision = &service.IdeaRevision{}
	if err := row.Scan(
		&post.ID,
		&post.AuthorUserID,
		&post.AuthorName,
		&post.CurrentRevisionID,
		&post.Status,
		&post.PublishedAt,
		&post.LikeCount,
		&post.FavoriteCount,
		&post.ViewCount,
		&post.CreatedAt,
		&post.UpdatedAt,
		&post.Revision.ID,
		&post.Revision.PostID,
		&post.Revision.RevisionNo,
		&post.Revision.Title,
		&post.Revision.Summary,
		&post.Revision.Body,
		&post.Revision.BodyHash,
		&post.Revision.ModerationStatus,
		&post.Revision.ModerationReason,
		&post.Revision.PublishedAt,
		&post.Revision.CreatedBy,
		&post.Revision.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &post, nil
}

func scanIdeaPostBase(row ideaRowScanner) (*service.IdeaPost, error) {
	var post service.IdeaPost
	post.Revision = &service.IdeaRevision{}
	if err := row.Scan(
		&post.ID,
		&post.AuthorUserID,
		&post.AuthorName,
		&post.CurrentRevisionID,
		&post.Status,
		&post.PublishedAt,
		&post.LikeCount,
		&post.FavoriteCount,
		&post.ViewCount,
		&post.CreatedAt,
		&post.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &post, nil
}

type ideaRowScanner interface {
	Scan(dest ...any) error
}

func normalizeIdeaPagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func slugifyIdeaTag(name string) string {
	return service.SlugifyIdeaTag(name)
}

func tagIDs(tags []service.IdeaTag) []int64 {
	out := make([]int64, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.ID)
	}
	return out
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// --- 互动 ---

func (r *ideasRepository) Like(ctx context.Context, postID, userID int64) (int, error) {
	return r.toggleIdeaInteraction(ctx, postID, userID, "idea_post_likes", "like_count", true)
}

func (r *ideasRepository) Unlike(ctx context.Context, postID, userID int64) (int, error) {
	return r.toggleIdeaInteraction(ctx, postID, userID, "idea_post_likes", "like_count", false)
}

func (r *ideasRepository) Favorite(ctx context.Context, postID, userID int64) (int, error) {
	return r.toggleIdeaInteraction(ctx, postID, userID, "idea_post_favorites", "favorite_count", true)
}

func (r *ideasRepository) Unfavorite(ctx context.Context, postID, userID int64) (int, error) {
	return r.toggleIdeaInteraction(ctx, postID, userID, "idea_post_favorites", "favorite_count", false)
}

// toggleIdeaInteraction 表名/计数字段均由本文件内部常量传入，不来自用户输入。
func (r *ideasRepository) toggleIdeaInteraction(ctx context.Context, postID, userID int64, table, countColumn string, add bool) (int, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer rollbackUnlessDone(tx)

	if add {
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (post_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, postID, userID); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE post_id = $1 AND user_id = $2`, postID, userID); err != nil {
			return 0, err
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE post_id = $1`, postID).Scan(&count); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE idea_posts SET `+countColumn+` = $1 WHERE id = $2`, count, postID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *ideasRepository) RecordView(ctx context.Context, postID, userID, revisionID int64) error {
	bucket := time.Now().Unix() / 900
	dedupKey := fmt.Sprintf("%d:%d:%d", userID, postID, bucket)
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO idea_post_views (post_id, user_id, revision_id, dedup_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (dedup_key) DO NOTHING`, postID, userID, revisionID, dedupKey)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, err = r.db.ExecContext(ctx, `UPDATE idea_posts SET view_count = view_count + 1 WHERE id = $1`, postID)
		return err
	}
	return nil
}

func (r *ideasRepository) GetInteractionState(ctx context.Context, postID, userID int64) (bool, bool, error) {
	var liked, favorited bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idea_post_likes WHERE post_id = $1 AND user_id = $2)`, postID, userID).Scan(&liked); err != nil {
		return false, false, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idea_post_favorites WHERE post_id = $1 AND user_id = $2)`, postID, userID).Scan(&favorited); err != nil {
		return false, false, err
	}
	return liked, favorited, nil
}

// --- 打赏 ---

func (r *ideasRepository) GetRewardByIdempotencyKey(ctx context.Context, key string) (*service.IdeaReward, error) {
	reward, err := scanIdeaReward(r.db.QueryRowContext(ctx, `
		SELECT id, payer_user_id, recipient_user_id, post_id, revision_id, asset_type, amount::double precision, status, created_at
		FROM idea_post_rewards WHERE idempotency_key = $1`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return reward, err
}

func (r *ideasRepository) CreateReward(ctx context.Context, input service.IdeaRewardInput, post *service.IdeaPost) (*service.IdeaReward, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	// 每用户每篇一次（跨资产类型，未逆转记录）
	var already bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM idea_post_rewards WHERE payer_user_id = $1 AND post_id = $2 AND status = 'completed')`,
		input.PayerUserID, input.PostID).Scan(&already); err != nil {
		return nil, err
	}
	if already {
		return nil, service.ErrIdeaAlreadyRewarded
	}

	// 稳定锁顺序（升序），避免并发打赏死锁
	lo, hi := input.PayerUserID, post.AuthorUserID
	if lo > hi {
		lo, hi = hi, lo
	}
	loBalance, loPoints, err := lockIdeaUserForUpdate(ctx, tx, lo)
	if err != nil {
		return nil, err
	}
	hiBalance, hiPoints, err := lockIdeaUserForUpdate(ctx, tx, hi)
	if err != nil {
		return nil, err
	}
	payerBalance, payerPoints := loBalance, loPoints
	recipientBalance, recipientPoints := hiBalance, hiPoints
	if lo != input.PayerUserID {
		payerBalance, recipientBalance = hiBalance, loBalance
		payerPoints, recipientPoints = hiPoints, loPoints
	}

	amount := input.Amount
	isBalance := input.AssetType == service.IdeaRewardAssetBalance
	if isBalance && payerBalance+1e-9 < amount {
		return nil, service.ErrIdeaInsufficientBalance
	}
	if !isBalance && payerPoints+1e-9 < amount {
		return nil, service.ErrIdeaInsufficientPoints
	}

	// 打赏业务记录
	var rewardID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO idea_post_rewards (payer_user_id, recipient_user_id, post_id, revision_id, asset_type, amount, idempotency_key, status)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, 'completed')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		input.PayerUserID, post.AuthorUserID, input.PostID, post.CurrentRevisionID, input.AssetType, decimalFromFloat(amount).StringFixed(10), input.IdempotencyKey,
	).Scan(&rewardID)
	if errors.Is(err, sql.ErrNoRows) {
		// 并发的同 key 打赏已提交: 回滚本事务并返回已存在记录, 不重复扣款。
		if rbErr := tx.Rollback(); rbErr != nil {
			return nil, rbErr
		}
		return r.GetRewardByIdempotencyKey(ctx, input.IdempotencyKey)
	} else if err != nil {
		return nil, err
	}

	metadata, _ := json.Marshal(map[string]any{"post_id": input.PostID, "asset_type": input.AssetType})

	if isBalance {
		payerNew := payerBalance - amount
		recipientNew := recipientBalance + amount
		if _, err := tx.ExecContext(ctx, `UPDATE users SET balance = $1::numeric, updated_at = NOW() WHERE id = $2`, decimalFromSignedFloat(payerNew).StringFixed(10), input.PayerUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET balance = $1::numeric, updated_at = NOW() WHERE id = $2`, decimalFromSignedFloat(recipientNew).StringFixed(10), post.AuthorUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_balance_ledger (user_id, direction, amount, reason, ref_type, ref_id, balance_after, metadata)
			VALUES ($1, 'debit', $2::numeric, 'idea_reward', 'idea_post_reward', $3, $4::numeric, $5::jsonb)`,
			input.PayerUserID, decimalFromFloat(amount).StringFixed(10), rewardID, decimalFromSignedFloat(payerNew).StringFixed(10), string(metadata)); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_balance_ledger (user_id, direction, amount, reason, ref_type, ref_id, balance_after, metadata)
			VALUES ($1, 'credit', $2::numeric, 'idea_reward', 'idea_post_reward', $3, $4::numeric, $5::jsonb)`,
			post.AuthorUserID, decimalFromFloat(amount).StringFixed(10), rewardID, decimalFromSignedFloat(recipientNew).StringFixed(10), string(metadata)); err != nil {
			return nil, err
		}
	} else {
		payerNew := payerPoints - amount
		recipientNew := recipientPoints + amount
		if _, err := tx.ExecContext(ctx, `UPDATE users SET points_balance = $1::numeric, updated_at = NOW() WHERE id = $2`, decimalFromSignedFloat(payerNew).StringFixed(10), input.PayerUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET points_balance = $1::numeric, updated_at = NOW() WHERE id = $2`, decimalFromSignedFloat(recipientNew).StringFixed(10), post.AuthorUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO points_ledger (user_id, direction, amount, reason, ref_type, ref_id, balance_before, balance_after, operator_user_id, metadata)
			VALUES ($1, 'debit', $2::numeric, 'idea_reward', 'idea_post_reward', $3, $4::numeric, $5::numeric, NULL, $6::jsonb)`,
			input.PayerUserID, decimalFromFloat(amount).StringFixed(10), rewardID, decimalFromSignedFloat(payerPoints).StringFixed(10), decimalFromSignedFloat(payerNew).StringFixed(10), string(metadata)); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO points_ledger (user_id, direction, amount, reason, ref_type, ref_id, balance_before, balance_after, operator_user_id, metadata)
			VALUES ($1, 'credit', $2::numeric, 'idea_reward', 'idea_post_reward', $3, $4::numeric, $5::numeric, NULL, $6::jsonb)`,
			post.AuthorUserID, decimalFromFloat(amount).StringFixed(10), rewardID, decimalFromSignedFloat(recipientPoints).StringFixed(10), decimalFromSignedFloat(recipientNew).StringFixed(10), string(metadata)); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.IdeaReward{
		ID:              rewardID,
		PayerUserID:     input.PayerUserID,
		RecipientUserID: post.AuthorUserID,
		PostID:          input.PostID,
		RevisionID:      post.CurrentRevisionID,
		AssetType:       input.AssetType,
		Amount:          amount,
		Status:          "completed",
		CreatedAt:       time.Now(),
	}, nil
}

func lockIdeaUserForUpdate(ctx context.Context, tx *sql.Tx, userID int64) (balance, points float64, err error) {
	err = tx.QueryRowContext(ctx, `
		SELECT balance::double precision, COALESCE(points_balance, 0)::double precision
		FROM users WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, userID).Scan(&balance, &points)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrUserNotFound
	}
	return balance, points, err
}

func scanIdeaReward(row ideaRowScanner) (*service.IdeaReward, error) {
	var r service.IdeaReward
	if err := row.Scan(&r.ID, &r.PayerUserID, &r.RecipientUserID, &r.PostID, &r.RevisionID, &r.AssetType, &r.Amount, &r.Status, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// --- 举报 ---

func (r *ideasRepository) CreateReport(ctx context.Context, postID, reporterUserID int64, reason, detail string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO idea_post_reports (post_id, reporter_user_id, reason, detail)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (post_id, reporter_user_id) WHERE status = 'pending' DO NOTHING`,
		postID, reporterUserID, reason, detail)
	return err
}

// --- 审核 ---

func (r *ideasRepository) ClaimPendingIdeaModerations(ctx context.Context, now time.Time, limit int) ([]service.IdeaModerationTarget, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	rows, err := tx.QueryContext(ctx, `
		SELECT r.post_id, r.id, r.revision_no, r.title, r.summary, r.body
		FROM idea_post_revisions r
		JOIN idea_posts p ON p.id = r.post_id
		WHERE r.moderation_status IN ('pending_review', 'pending_revision')
			AND p.deleted_at IS NULL
			AND (r.moderation_next_retry_at IS NULL OR r.moderation_next_retry_at <= $1)
		ORDER BY r.id
		LIMIT $2
		FOR UPDATE OF r SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	targets := make([]service.IdeaModerationTarget, 0, limit)
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var t service.IdeaModerationTarget
		if err := rows.Scan(&t.PostID, &t.RevisionID, &t.RevisionNo, &t.Title, &t.Summary, &t.Body); err != nil {
			_ = rows.Close()
			return nil, err
		}
		targets = append(targets, t)
		ids = append(ids, t.RevisionID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	if len(ids) == 0 {
		_ = tx.Rollback()
		return nil, nil
	}
	// 领取即递增尝试次数，并把下次重试推到未来，避免并发重复领取
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_post_revisions
		SET moderation_attempts = moderation_attempts + 1,
			moderation_next_retry_at = NOW() + INTERVAL '5 minutes'
		WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// 领取成功后补充标签 slug，供标签黑名单兜底判断。
	for i := range targets {
		tags, err := r.loadTags(ctx, targets[i].PostID)
		if err != nil {
			return nil, err
		}
		for _, tg := range tags {
			targets[i].Tags = append(targets[i].Tags, tg.Slug)
		}
	}
	return targets, nil
}

func (r *ideasRepository) ApplyModerationDecision(ctx context.Context, postID, revisionID int64, decision, riskLevel, reason, model, url, promptVersion string) (*service.IdeaPost, error) {
	if err := r.insertIdeaModerationEvent(ctx, postID, revisionID, "ai", decision, riskLevel, reason, model, url, promptVersion, "", 0); err != nil {
		return nil, err
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE idea_post_revisions SET moderation_next_retry_at = NULL WHERE id = $1`, revisionID); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "pass":
		return r.SetPostStatus(ctx, postID, service.IdeaPostStatusPublished, service.IdeaRevisionStatusApproved, "", 0, true)
	case "review":
		return r.SetPostStatus(ctx, postID, service.IdeaPostStatusManualReview, service.IdeaRevisionStatusManualReview, "", 0, false)
	case "reject":
		return r.SetPostStatus(ctx, postID, service.IdeaPostStatusRejected, service.IdeaRevisionStatusRejected, reason, 0, false)
	default:
		return nil, fmt.Errorf("invalid moderation decision %q", decision)
	}
}

func (r *ideasRepository) FailIdeaModeration(ctx context.Context, postID, revisionID int64, errMsg, model, url string, nextRetryAt time.Time, maxAttempts int) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollbackUnlessDone(tx)

	// 先锁 post 行, 与 SetPostStatus/PublishRevision 保持一致锁序(post -> revision), 避免 AB-BA 死锁。
	if _, err := getIdeaPostForUpdate(ctx, tx, postID); err != nil {
		return err
	}
	if err := insertIdeaModerationEventTx(ctx, tx, postID, revisionID, "ai", "failed", "", "", model, url, "", errMsg, 0); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_post_revisions SET moderation_next_retry_at = $1 WHERE id = $2`,
		nextRetryAt, revisionID); err != nil {
		return err
	}
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT moderation_attempts FROM idea_post_revisions WHERE id = $1`, revisionID).Scan(&attempts); err != nil {
		return err
	}
	if attempts >= maxAttempts {
		if _, err := tx.ExecContext(ctx, `
			UPDATE idea_post_revisions SET moderation_status = 'moderation_failed' WHERE id = $1`, revisionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE idea_posts SET status = 'moderation_failed', updated_at = NOW() WHERE id = $1`, postID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *ideasRepository) insertIdeaModerationEvent(ctx context.Context, postID, revisionID int64, stage, decision, riskLevel, reason, model, url, promptVersion, lastError string, attemptCount int) error {
	return insertIdeaModerationEventTx(ctx, r.db, postID, revisionID, stage, decision, riskLevel, reason, model, url, promptVersion, lastError, attemptCount)
}

func insertIdeaModerationEventTx(ctx context.Context, exec sqlExecutor, postID, revisionID int64, stage, decision, riskLevel, reason, model, url, promptVersion, lastError string, attemptCount int) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO idea_moderation_events (post_id, revision_id, stage, decision, risk_level, reason, model_snapshot, url_snapshot, prompt_version, last_error, attempt_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		postID, revisionID, stage, decision, nullIfEmpty(riskLevel), nullIfEmpty(reason), nullIfEmpty(model), nullIfEmpty(url), nullIfEmpty(promptVersion), nullIfEmpty(lastError), attemptCount)
	return err
}

// --- 举报管理 ---

func (r *ideasRepository) ListReports(ctx context.Context, status string, page, pageSize int) ([]service.IdeaReport, int64, error) {
	page, pageSize = normalizeIdeaPagination(page, pageSize)
	where := ""
	args := []any{}
	if s := strings.TrimSpace(status); s != "" && s != "all" {
		args = append(args, s)
		where = fmt.Sprintf(" WHERE rep.status = $%d", len(args))
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idea_post_reports rep`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT rep.id, rep.post_id, COALESCE(cur.title, ''), rep.reporter_user_id, rep.reason, rep.detail,
			rep.status, rep.handled_by_user_id, COALESCE(rep.resolution, ''), rep.created_at
		FROM idea_post_reports rep
		LEFT JOIN idea_posts p ON p.id = rep.post_id
		LEFT JOIN idea_post_revisions cur ON cur.id = p.current_revision_id
		%s
		ORDER BY rep.created_at DESC, rep.id DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.IdeaReport, 0)
	for rows.Next() {
		var rep service.IdeaReport
		if err := rows.Scan(&rep.ID, &rep.PostID, &rep.PostTitle, &rep.ReporterUserID, &rep.Reason, &rep.Detail,
			&rep.Status, &rep.HandledByUserID, &rep.Resolution, &rep.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, rep)
	}
	return items, total, rows.Err()
}

func (r *ideasRepository) ResolveReport(ctx context.Context, reportID, adminUserID int64, resolution string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE idea_post_reports
		SET status = 'resolved', handled_by_user_id = $2, resolution = $3, updated_at = NOW()
		WHERE id = $1`, reportID, adminUserID, resolution)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return service.ErrIdeaPostNotFound
	}
	return nil
}

// --- 标签治理 ---

func (r *ideasRepository) ListAllTags(ctx context.Context) ([]service.IdeaTag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ideaTagColumns+`
		FROM idea_tags
		ORDER BY sort_order DESC, usage_count DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := make([]service.IdeaTag, 0)
	for rows.Next() {
		var t service.IdeaTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (r *ideasRepository) CreateTag(ctx context.Context, name string) (*service.IdeaTag, error) {
	slug := slugifyIdeaTag(name)
	if slug == "" {
		return nil, service.ErrIdeaTagNameInvalid
	}
	var t service.IdeaTag
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO idea_tags (name, slug)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET status = 'active', updated_at = NOW()
		RETURNING `+ideaTagColumns, name, slug,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *ideasRepository) UpdateTag(ctx context.Context, tagID int64, name, status string) (*service.IdeaTag, error) {
	var t service.IdeaTag
	if err := r.db.QueryRowContext(ctx, `SELECT `+ideaTagColumns+` FROM idea_tags WHERE id = $1`, tagID).
		Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.SortOrder, &t.UsageCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrIdeaTagNotFound
		}
		return nil, err
	}
	if n := strings.TrimSpace(name); n != "" {
		t.Name = n
		t.Slug = slugifyIdeaTag(n)
	}
	if status == "active" || status == "disabled" {
		t.Status = status
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE idea_tags SET name = $1, slug = $2, status = $3, updated_at = NOW() WHERE id = $4`,
		t.Name, t.Slug, t.Status, tagID); err != nil {
		// 改名后 slug 与既有标签冲突时转为参数错误, 避免裸 500。
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, service.ErrIdeaTagNameInvalid
		}
		return nil, err
	}
	return &t, nil
}

func (r *ideasRepository) MergeTags(ctx context.Context, sourceTagID, targetTagID int64) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollbackUnlessDone(tx)

	// 锁定目标标签并读取 slug，用于写入 source 的重定向。
	var targetSlug string
	if err := tx.QueryRowContext(ctx, `SELECT slug FROM idea_tags WHERE id = $1 FOR UPDATE`, targetTagID).
		Scan(&targetSlug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrIdeaTagNotFound
		}
		return err
	}
	var sourceExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM idea_tags WHERE id = $1 FOR UPDATE`, sourceTagID).
		Scan(&sourceExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrIdeaTagNotFound
		}
		return err
	}

	// 迁移关联：source 下的文章-标签关系并入 target，已存在的跳过。
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idea_post_tags (post_id, tag_id)
		SELECT pt.post_id, $2
		FROM idea_post_tags pt
		WHERE pt.tag_id = $1
		ON CONFLICT (post_id, tag_id) DO NOTHING`,
		sourceTagID, targetTagID); err != nil {
		return err
	}

	// 删除 source 的旧关联。
	if _, err := tx.ExecContext(ctx, `DELETE FROM idea_post_tags WHERE tag_id = $1`, sourceTagID); err != nil {
		return err
	}

	// 停用 source 并记录 slug 重定向，让旧标签链接仍可解析到 target。
	if _, err := tx.ExecContext(ctx, `
		UPDATE idea_tags SET status = 'disabled', redirect_to_slug = $2, updated_at = NOW() WHERE id = $1`,
		sourceTagID, targetSlug); err != nil {
		return err
	}

	// 重算受影响标签的 usage_count。
	for _, id := range []int64{sourceTagID, targetTagID} {
		if _, err := tx.ExecContext(ctx, `
			UPDATE idea_tags SET usage_count = (
				SELECT COUNT(*) FROM idea_post_tags WHERE idea_post_tags.tag_id = idea_tags.id
			)
			WHERE id = $1`, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}
