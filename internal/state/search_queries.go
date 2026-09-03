package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	generated "github.com/johannhipp/kcli/internal/state/sqlc"
)

// SearchSellerRecord is the public seller and listing snapshot observed in a
// decoded search response.
type SearchSellerRecord struct {
	SellerID      string
	DisplayName   string
	PublicJSON    json.RawMessage
	Source        string
	Completeness  string
	ListingID     string
	ListingTitle  string
	ListingStatus string
	ListingURL    string
	ObservedAt    time.Time
}

// SearchUpsertSellers atomically updates every seller and seller/listing link
// observed in one decoded search page.
func (d *DB) SearchUpsertSellers(ctx context.Context, records []SearchSellerRecord) error {
	if d == nil {
		return fmt.Errorf("state database is unavailable")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *generated.Queries) error {
		for _, record := range records {
			if record.SellerID == "" || record.ListingID == "" {
				continue
			}
			observedAt := record.ObservedAt.UTC().Format(time.RFC3339Nano)
			if _, err := tx.ExecContext(ctx, `INSERT INTO sellers(seller_id,folded_name,display_name,public_json,source,completeness,observed_at)
				VALUES(?,?,?,?,?,?,?)
				ON CONFLICT(seller_id) DO UPDATE SET folded_name=excluded.folded_name,display_name=excluded.display_name,public_json=excluded.public_json,source=excluded.source,completeness=excluded.completeness,observed_at=excluded.observed_at`,
				record.SellerID, searchFoldSellerName(record.DisplayName), record.DisplayName, []byte(record.PublicJSON), record.Source, record.Completeness, observedAt); err != nil {
				return fmt.Errorf("upsert seller %q: %w", record.SellerID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO seller_listings(seller_id,listing_id,title,status,url,observed_at)
				VALUES(?,?,?,?,?,?)
				ON CONFLICT(seller_id,listing_id) DO UPDATE SET title=excluded.title,status=excluded.status,url=excluded.url,observed_at=excluded.observed_at`,
				record.SellerID, record.ListingID, record.ListingTitle, record.ListingStatus, record.ListingURL, observedAt); err != nil {
				return fmt.Errorf("upsert seller listing %q: %w", record.ListingID, err)
			}
		}
		return nil
	})
}

func searchFoldSellerName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(norm.NFKC.String(value)))
}
