package state

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
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
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		var newest time.Time
		for _, record := range records {
			if record.SellerID == "" || record.ListingID == "" {
				continue
			}
			if record.ObservedAt.After(newest) {
				newest = record.ObservedAt
			}
			var previousName, previousSource, previousCompleteness string
			var previousJSON []byte
			err := tx.QueryRowContext(ctx, `SELECT display_name,public_json,source,completeness FROM sellers WHERE seller_id=?`, record.SellerID).Scan(&previousName, &previousJSON, &previousSource, &previousCompleteness)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if err == nil {
				// Detail/profile names outrank names embedded in search previews.
				preserveName := previousName != "" && (previousSource == "listing" || previousSource == "public-web")
				preferredName := ""
				if preserveName {
					preferredName = previousName
				}
				merged, err := mergeSellerPublic(previousJSON, record.PublicJSON, preferredName)
				if err != nil {
					return err
				}
				record.PublicJSON = merged
				if preserveName || record.DisplayName == "" {
					record.DisplayName = previousName
					record.Source = previousSource
					record.Completeness = previousCompleteness
				}
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
		if newest.IsZero() {
			return nil
		}
		return pruneSellers(ctx, tx, newest)
	})
}

func searchFoldSellerName(value string) string {
	return cases.Fold().String(norm.NFKC.String(strings.TrimSpace(value)))
}

// Sparse search previews must not erase richer profile or listing observations.
func mergeSellerPublic(previous, incoming []byte, preferredName string) (json.RawMessage, error) {
	decode := func(raw []byte) (map[string]any, error) {
		value := map[string]any{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&value)
		return value, err
	}
	old, err := decode(previous)
	if err != nil {
		return nil, err
	}
	fresh, err := decode(incoming)
	if err != nil {
		return nil, err
	}
	if old == nil {
		old = map[string]any{}
	}
	var merge func(map[string]any, map[string]any)
	merge = func(dst, src map[string]any) {
		for key, value := range src {
			if value == nil {
				continue
			}
			switch typed := value.(type) {
			case string:
				if typed == "" {
					continue
				}
			case []any:
				if len(typed) == 0 {
					continue
				}
			case map[string]any:
				if len(typed) == 0 {
					continue
				}
				if nested, ok := dst[key].(map[string]any); ok && nested != nil {
					merge(nested, typed)
					continue
				}
			}
			dst[key] = value
		}
	}
	merge(old, fresh)
	if preferredName != "" {
		old["contact-name"] = preferredName
	}
	return json.Marshal(old)
}
