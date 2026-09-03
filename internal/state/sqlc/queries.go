// Code generated in the sqlc-compatible shape for the Phase 1 bootstrap. DO NOT EDIT.

package sqlc

import (
	"context"
	"database/sql"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Queries struct{ db DBTX }

func New(db DBTX) *Queries                    { return &Queries{db: db} }
func (q *Queries) WithTx(tx *sql.Tx) *Queries { return &Queries{db: tx} }

func (q *Queries) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := q.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	return value, err
}
func (q *Queries) SetMeta(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
func (q *Queries) DeleteMeta(ctx context.Context, key string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, key)
	return err
}
func (q *Queries) GetRateSlot(ctx context.Context, host string) (int64, error) {
	var value int64
	err := q.db.QueryRowContext(ctx, `SELECT next_eligible_at_ms FROM rate_slots WHERE host = ?`, host).Scan(&value)
	return value, err
}
func (q *Queries) UpsertRateSlot(ctx context.Context, host string, next int64) error {
	_, err := q.db.ExecContext(ctx, `INSERT INTO rate_slots (host, next_eligible_at_ms) VALUES (?, ?) ON CONFLICT(host) DO UPDATE SET next_eligible_at_ms = excluded.next_eligible_at_ms`, host, next)
	return err
}

type Lease struct {
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	ExpiresAtMs int64  `json:"expires_at_ms"`
}

func (q *Queries) GetLease(ctx context.Context, name string) (Lease, error) {
	var lease Lease
	err := q.db.QueryRowContext(ctx, `SELECT name, owner, expires_at_ms FROM leases WHERE name = ?`, name).Scan(&lease.Name, &lease.Owner, &lease.ExpiresAtMs)
	return lease, err
}
func (q *Queries) DeleteLease(ctx context.Context, name, owner string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM leases WHERE name = ? AND owner = ?`, name, owner)
	return err
}
func (q *Queries) ListSchemaTables(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
