package state

import "context"

// PublicListingURL resolves an exact previously encountered public listing URL.
// An arbitrary ID cannot safely be expanded into an undocumented website path.
func (d *DB) PublicListingURL(ctx context.Context, id string) (string, error) {
	var result string
	err := d.sql.QueryRowContext(ctx, `SELECT url FROM seller_listings WHERE listing_id=? AND url<>'' ORDER BY observed_at DESC LIMIT 1`, id).Scan(&result)
	return result, err
}
