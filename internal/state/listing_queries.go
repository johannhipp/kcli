package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	listingSellerRetention = 30 * 24 * time.Hour
	listingSellerMaximum   = 10_000
)

type SellerSnapshot struct {
	ID           string
	FoldedName   string
	DisplayName  string
	PublicJSON   json.RawMessage
	Source       string
	Completeness string
	ObservedAt   time.Time
}

type SellerListingSnapshot struct {
	SellerID   string
	ListingID  string
	Title      string
	Status     string
	URL        string
	ObservedAt time.Time
}

type SellerIndexInfo struct {
	Size             int
	OldestObservedAt time.Time
	NewestObservedAt time.Time
}

// UpsertPublicSeller records a profile independently of any listing, under the
// same retention limits as sellers observed on listing pages.
func (d *DB) UpsertPublicSeller(ctx context.Context, seller SellerSnapshot) error {
	if err := listingValidateSellerSnapshot(seller); err != nil {
		return err
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		observed := seller.ObservedAt.UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO sellers(seller_id,folded_name,display_name,public_json,source,completeness,observed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(seller_id) DO UPDATE SET folded_name=excluded.folded_name,display_name=excluded.display_name,public_json=excluded.public_json,source=excluded.source,completeness=excluded.completeness,observed_at=excluded.observed_at`, seller.ID, seller.FoldedName, seller.DisplayName, []byte(seller.PublicJSON), seller.Source, seller.Completeness, observed); err != nil {
			return err
		}
		cutoff := seller.ObservedAt.UTC().Add(-listingSellerRetention).Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE observed_at < ?`, cutoff); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE seller_id IN (SELECT seller_id FROM sellers ORDER BY observed_at DESC,seller_id LIMIT -1 OFFSET ?)`, listingSellerMaximum)
		return err
	})
}

func (d *DB) UpsertSellerListing(ctx context.Context, seller SellerSnapshot, listing SellerListingSnapshot) error {
	if err := listingValidateSellerSnapshot(seller); err != nil {
		return err
	}
	if listing.SellerID != seller.ID || listing.ListingID == "" || listing.ObservedAt.IsZero() {
		return fmt.Errorf("invalid seller listing snapshot")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		observed := seller.ObservedAt.UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO sellers(seller_id,folded_name,display_name,public_json,source,completeness,observed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(seller_id) DO UPDATE SET folded_name=excluded.folded_name,display_name=excluded.display_name,public_json=excluded.public_json,source=excluded.source,completeness=excluded.completeness,observed_at=excluded.observed_at`, seller.ID, seller.FoldedName, seller.DisplayName, []byte(seller.PublicJSON), seller.Source, seller.Completeness, observed); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO seller_listings(seller_id,listing_id,title,status,url,observed_at) VALUES(?,?,?,?,?,?) ON CONFLICT(seller_id,listing_id) DO UPDATE SET title=excluded.title,status=excluded.status,url=excluded.url,observed_at=excluded.observed_at`, listing.SellerID, listing.ListingID, listing.Title, listing.Status, listing.URL, listing.ObservedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		cutoff := seller.ObservedAt.UTC().Add(-listingSellerRetention).Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE observed_at < ?`, cutoff); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sellers WHERE seller_id IN (SELECT seller_id FROM sellers ORDER BY observed_at DESC,seller_id LIMIT -1 OFFSET ?)`, listingSellerMaximum)
		return err
	})
}

func (d *DB) SellerByID(ctx context.Context, id string) (SellerSnapshot, error) {
	var item SellerSnapshot
	var raw []byte
	var observed string
	err := d.sql.QueryRowContext(ctx, `SELECT seller_id,folded_name,display_name,public_json,source,completeness,observed_at FROM sellers WHERE seller_id=?`, id).Scan(&item.ID, &item.FoldedName, &item.DisplayName, &raw, &item.Source, &item.Completeness, &observed)
	if err != nil {
		return item, err
	}
	item.PublicJSON = append(json.RawMessage(nil), raw...)
	item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
	if err != nil {
		return item, fmt.Errorf("parse seller observation time: %w", err)
	}
	return item, nil
}

func (d *DB) SearchSellers(ctx context.Context, foldedName, match string, limit int) ([]SellerSnapshot, SellerIndexInfo, error) {
	if foldedName == "" || (match != "exact" && match != "contains") || limit < 1 || limit > 100 {
		return nil, SellerIndexInfo{}, fmt.Errorf("invalid seller search")
	}
	info, err := d.SellerIndexInfo(ctx)
	if err != nil {
		return nil, info, err
	}
	query := `SELECT seller_id,folded_name,display_name,public_json,source,completeness,observed_at FROM sellers WHERE folded_name=? ORDER BY observed_at DESC,seller_id LIMIT ?`
	argument := foldedName
	if match == "contains" {
		query = `SELECT seller_id,folded_name,display_name,public_json,source,completeness,observed_at FROM sellers WHERE folded_name LIKE ? ESCAPE '\' ORDER BY observed_at DESC,seller_id LIMIT ?`
		argument = "%" + listingEscapeLike(foldedName) + "%"
	}
	rows, err := d.sql.QueryContext(ctx, query, argument, limit)
	if err != nil {
		return nil, info, err
	}
	defer rows.Close()
	items := make([]SellerSnapshot, 0)
	for rows.Next() {
		var item SellerSnapshot
		var raw []byte
		var observed string
		if err := rows.Scan(&item.ID, &item.FoldedName, &item.DisplayName, &raw, &item.Source, &item.Completeness, &observed); err != nil {
			return nil, info, err
		}
		item.PublicJSON = append(json.RawMessage(nil), raw...)
		item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, info, fmt.Errorf("parse seller observation time: %w", err)
		}
		items = append(items, item)
	}
	return items, info, rows.Err()
}

func (d *DB) SellerListings(ctx context.Context, sellerID string, limit int) ([]SellerListingSnapshot, error) {
	if sellerID == "" || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid seller listing lookup")
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT seller_id,listing_id,title,status,url,observed_at FROM seller_listings WHERE seller_id=? ORDER BY observed_at DESC,listing_id LIMIT ?`, sellerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SellerListingSnapshot, 0)
	for rows.Next() {
		var item SellerListingSnapshot
		var observed string
		if err := rows.Scan(&item.SellerID, &item.ListingID, &item.Title, &item.Status, &item.URL, &observed); err != nil {
			return nil, err
		}
		item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, fmt.Errorf("parse seller listing observation time: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) SellerIndexInfo(ctx context.Context) (SellerIndexInfo, error) {
	var info SellerIndexInfo
	var oldest, newest sql.NullString
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*),MIN(observed_at),MAX(observed_at) FROM sellers`).Scan(&info.Size, &oldest, &newest); err != nil {
		return info, err
	}
	var err error
	if oldest.Valid {
		info.OldestObservedAt, err = time.Parse(time.RFC3339Nano, oldest.String)
		if err != nil {
			return info, fmt.Errorf("parse seller index horizon: %w", err)
		}
	}
	if newest.Valid {
		info.NewestObservedAt, err = time.Parse(time.RFC3339Nano, newest.String)
		if err != nil {
			return info, fmt.Errorf("parse seller index latest observation: %w", err)
		}
	}
	return info, nil
}

func listingValidateSellerSnapshot(item SellerSnapshot) error {
	if item.ID == "" || item.FoldedName == "" || item.DisplayName == "" || len(item.PublicJSON) == 0 || !json.Valid(item.PublicJSON) || item.Source == "" || item.Completeness == "" || item.ObservedAt.IsZero() {
		return fmt.Errorf("invalid seller snapshot")
	}
	return nil
}

func listingEscapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
