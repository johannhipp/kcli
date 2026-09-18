package state

import (
	"context"
	"database/sql"
	"time"
)

const (
	listingSellerRetention = 30 * 24 * time.Hour
	listingSellerMaximum   = 10_000
)

// pruneSellers applies the same retention policy inside every seller-write
// transaction. Foreign-key cascades remove the corresponding listing links.
func pruneSellers(ctx context.Context, tx *sql.Tx, observedAt time.Time) error {
	cutoff := observedAt.UTC().Add(-listingSellerRetention).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE observed_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE seller_id IN (SELECT seller_id FROM sellers ORDER BY observed_at DESC,seller_id LIMIT -1 OFFSET ?)`, listingSellerMaximum)
	return err
}
