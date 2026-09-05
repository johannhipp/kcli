package state

import (
	"context"
	"database/sql"
)

// DBTX is the minimal database handle shared by every hand-written query
// method. It is satisfied by *sql.DB and *sql.Tx so the same queries run on
// the connection and inside a transaction.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Queries is the hand-maintained, typed SQLite query layer. Data access in the
// state package deliberately uses plain SQL statements rather than a code
// generator: the account/cursor/confirmation/reconciliation queries are
// domain-shaped and reference SQLite internals (for example sqlite_schema) that
// a code generator cannot resolve from the migration catalogue alone.
type Queries struct {
	db DBTX
}

// NewQueries binds a query layer to a database handle (*sql.DB in production,
// *sql.Tx inside WithTx).
func NewQueries(db DBTX) *Queries { return &Queries{db: db} }

// WithTx returns a copy of the query layer bound to tx, so the same methods
// run inside a transaction.
func (q *Queries) WithTx(tx *sql.Tx) *Queries { return &Queries{db: tx} }

// Lease describes one per-profile synchronization lease row.
type Lease struct {
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	ExpiresAtMs int64  `json:"expires_at_ms"`
}

// GetMeta returns the value stored for key, or sql.ErrNoRows when absent.
func (q *Queries) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := q.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	return value, err
}

// SetMeta inserts or updates the meta value for key.
func (q *Queries) SetMeta(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// DeleteMeta removes the meta value for key.
func (q *Queries) DeleteMeta(ctx context.Context, key string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, key)
	return err
}

// GetRateSlot returns the next eligible request time for host, or
// sql.ErrNoRows when no reservation exists yet.
func (q *Queries) GetRateSlot(ctx context.Context, host string) (int64, error) {
	var value int64
	err := q.db.QueryRowContext(ctx, `SELECT next_eligible_at_ms FROM rate_slots WHERE host = ?`, host).Scan(&value)
	return value, err
}

// UpsertRateSlot sets the next eligible request time for host.
func (q *Queries) UpsertRateSlot(ctx context.Context, host string, next int64) error {
	_, err := q.db.ExecContext(ctx, `INSERT INTO rate_slots (host, next_eligible_at_ms) VALUES (?, ?) ON CONFLICT(host) DO UPDATE SET next_eligible_at_ms = excluded.next_eligible_at_ms`, host, next)
	return err
}

// GetLease returns the lease named name, or sql.ErrNoRows when absent.
func (q *Queries) GetLease(ctx context.Context, name string) (Lease, error) {
	var lease Lease
	err := q.db.QueryRowContext(ctx, `SELECT name, owner, expires_at_ms FROM leases WHERE name = ?`, name).Scan(&lease.Name, &lease.Owner, &lease.ExpiresAtMs)
	return lease, err
}

// DeleteLease removes the lease named name owned by owner.
func (q *Queries) DeleteLease(ctx context.Context, name, owner string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM leases WHERE name = ? AND owner = ?`, name, owner)
	return err
}
