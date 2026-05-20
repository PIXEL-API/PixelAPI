package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "modernc.org/sqlite"
)

type UsageQueue struct {
	db *sql.DB
}

type UsageQueueItem struct {
	ID        int64
	Payload   service.UsageIngestItem
	CreatedAt time.Time
}

func Open(path string) (*UsageQueue, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite queue: %w", err)
	}
	q := &UsageQueue{db: db}
	if err := q.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return q, nil
}

func (q *UsageQueue) Close() error {
	if q == nil || q.db == nil {
		return nil
	}
	return q.db.Close()
}

func (q *UsageQueue) Enqueue(ctx context.Context, item service.UsageIngestItem) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal usage queue item: %w", err)
	}
	_, err = q.db.ExecContext(ctx, `
		INSERT INTO usage_queue (request_id, reservation_id, payload, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(request_id, reservation_id) DO UPDATE SET
			payload = excluded.payload,
			updated_at = CURRENT_TIMESTAMP
	`, item.RequestID, item.ReservationID, string(payload))
	if err != nil {
		return fmt.Errorf("enqueue usage item: %w", err)
	}
	return nil
}

func (q *UsageQueue) DequeueBatch(ctx context.Context, limit int) ([]UsageQueueItem, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, payload, created_at
		FROM usage_queue
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("select usage queue: %w", err)
	}
	defer rows.Close()
	items := make([]UsageQueueItem, 0, limit)
	for rows.Next() {
		var item UsageQueueItem
		var raw string
		if err := rows.Scan(&item.ID, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Payload); err != nil {
			return nil, fmt.Errorf("parse usage queue item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *UsageQueue) Ack(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage queue ack: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM usage_queue WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare usage queue ack: %w", err)
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return fmt.Errorf("ack usage queue item: %w", err)
		}
	}
	return tx.Commit()
}

func (q *UsageQueue) Depth(ctx context.Context) (int, error) {
	var count int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_queue`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count usage queue: %w", err)
	}
	return count, nil
}

func (q *UsageQueue) init(ctx context.Context) error {
	_, err := q.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS usage_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			reservation_id TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(request_id, reservation_id)
		);
		CREATE INDEX IF NOT EXISTS idx_usage_queue_created_at ON usage_queue(created_at);
	`)
	if err != nil {
		return fmt.Errorf("init usage queue: %w", err)
	}
	return nil
}
