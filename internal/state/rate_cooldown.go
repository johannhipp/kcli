package state

import (
	"context"
	"database/sql"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

// SetRateCooldown never shortens a cooldown already observed by another client.
func (d *DB) SetRateCooldown(ctx context.Context, host string, until time.Time) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO rate_cooldowns(host,not_before_ms) VALUES(?,?)
 ON CONFLICT(host) DO UPDATE SET not_before_ms=MAX(rate_cooldowns.not_before_ms,excluded.not_before_ms)`, host, until.UnixMilli())
	return err
}

// CheckRateCooldown is called before reserving and again immediately before
// dispatch. A queued client must stop, not retry, when another client sees 429.
func (d *DB) CheckRateCooldown(ctx context.Context, host string, now time.Time) error {
	var until int64
	err := d.sql.QueryRowContext(ctx, `SELECT not_before_ms FROM rate_cooldowns WHERE host=?`, host).Scan(&until)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	delay := time.UnixMilli(until).Sub(now)
	if delay <= 0 {
		return nil
	}
	return &domain.Error{Code: domain.CodeRateLimited, Message: "public website cooldown is active; no request was sent", Retryable: true, RetryAfter: &delay}
}
