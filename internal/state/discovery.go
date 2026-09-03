package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	generated "github.com/johannhipp/kcli/internal/state/sqlc"
)

const minimumHostSpacing = 2500 * time.Millisecond

type CategorySnapshot struct {
	ID         string
	Path       string
	Label      string
	ParentID   string
	RawJSON    json.RawMessage
	ObservedAt time.Time
}

type FilterSnapshot struct {
	CategoryID  string
	Key         string
	Type        string
	SearchParam string
	SearchStyle string
	RawJSON     json.RawMessage
	ObservedAt  time.Time
	Proof       string
}

type LocationCacheEntry struct {
	QueryKey   string
	ResultJSON json.RawMessage
	ObservedAt time.Time
	ExpiresAt  time.Time
}

func (d *DB) MobileInstallID(ctx context.Context, now time.Time) (string, error) {
	var id string
	err := d.WithTx(ctx, func(tx *sql.Tx, q *generated.Queries) error {
		uuid, err := q.GetMeta(ctx, "profile_uuid")
		if err != nil {
			return err
		}
		created, err := q.GetMeta(ctx, "mobile_install_created_ms")
		if err == sql.ErrNoRows {
			created = strconv.FormatInt(now.UTC().UnixMilli(), 10)
			if err := q.SetMeta(ctx, "mobile_install_created_ms", created); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := strconv.ParseInt(created, 10, 64); err != nil {
			return fmt.Errorf("invalid stored mobile install creation time")
		}
		id = uuid + created
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("load mobile install identity: %w", err)
	}
	return id, nil
}

// ReserveRateSlot atomically reserves one request start time. It returns how
// long the caller must wait before issuing the request.
func (d *DB) ReserveRateSlot(ctx context.Context, host string, now time.Time, jitter, maxQueue time.Duration) (time.Duration, error) {
	if host == "" || len(host) > 128 || strings.IndexFunc(host, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return 0, fmt.Errorf("invalid rate-limit host")
	}
	if jitter < 0 || jitter > time.Second {
		return 0, fmt.Errorf("rate-limit jitter must be between zero and one second")
	}
	nowMS := now.UTC().UnixMilli()
	var wait time.Duration
	err := d.WithTx(ctx, func(_ *sql.Tx, q *generated.Queries) error {
		next, err := q.GetRateSlot(ctx, host)
		if err == sql.ErrNoRows {
			next = nowMS
		} else if err != nil {
			return err
		}
		start := next
		if start < nowMS {
			start = nowMS
		}
		wait = time.Duration(start-nowMS) * time.Millisecond
		if maxQueue > 0 && wait > maxQueue {
			retryAfter := wait
			return &domain.Error{Code: domain.CodeRateLimitedLocal, Message: "local request queue exceeds 30 seconds", Retryable: true, RetryAfter: &retryAfter, Details: map[string]any{"host": host}}
		}
		return q.UpsertRateSlot(ctx, host, start+minimumHostSpacing.Milliseconds()+jitter.Milliseconds())
	})
	if err != nil {
		return 0, err
	}
	return wait, nil
}

func (d *DB) MoveRateSlot(ctx context.Context, host string, notBefore time.Time) error {
	return d.WithTx(ctx, func(_ *sql.Tx, q *generated.Queries) error {
		next, err := q.GetRateSlot(ctx, host)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		candidate := notBefore.UTC().UnixMilli()
		if next > candidate {
			candidate = next
		}
		return q.UpsertRateSlot(ctx, host, candidate)
	})
}

func (d *DB) ReplaceCategories(ctx context.Context, categories []CategorySnapshot) error {
	return d.WithTx(ctx, func(tx *sql.Tx, _ *generated.Queries) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM category_snapshots`); err != nil {
			return err
		}
		for _, category := range categories {
			if _, err := tx.ExecContext(ctx, `INSERT INTO category_snapshots(id,path,label,parent_id,raw_json,observed_at) VALUES(?,?,?,?,?,?)`, category.ID, category.Path, category.Label, nullString(category.ParentID), []byte(category.RawJSON), category.ObservedAt.UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListCategories(ctx context.Context) ([]CategorySnapshot, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id,path,label,COALESCE(parent_id,''),raw_json,observed_at FROM category_snapshots ORDER BY path COLLATE NOCASE,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategorySnapshot
	for rows.Next() {
		var item CategorySnapshot
		var raw []byte
		var observed string
		if err := rows.Scan(&item.ID, &item.Path, &item.Label, &item.ParentID, &raw, &observed); err != nil {
			return nil, err
		}
		item.RawJSON = append(json.RawMessage(nil), raw...)
		item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, fmt.Errorf("parse category observation time: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) ReplaceFilters(ctx context.Context, categoryID string, filters []FilterSnapshot) error {
	return d.WithTx(ctx, func(tx *sql.Tx, _ *generated.Queries) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM filter_snapshots WHERE category_id=?`, categoryID); err != nil {
			return err
		}
		for _, filter := range filters {
			if _, err := tx.ExecContext(ctx, `INSERT INTO filter_snapshots(category_id,key,type,search_param,search_style,raw_json,observed_at,proof) VALUES(?,?,?,?,?,?,?,?)`, categoryID, filter.Key, filter.Type, filter.SearchParam, filter.SearchStyle, []byte(filter.RawJSON), filter.ObservedAt.UTC().Format(time.RFC3339Nano), filter.Proof); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListFilters(ctx context.Context, categoryID string) ([]FilterSnapshot, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT category_id,key,type,search_param,search_style,raw_json,observed_at,proof FROM filter_snapshots WHERE category_id=? ORDER BY key COLLATE NOCASE`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FilterSnapshot
	for rows.Next() {
		var item FilterSnapshot
		var raw []byte
		var observed string
		if err := rows.Scan(&item.CategoryID, &item.Key, &item.Type, &item.SearchParam, &item.SearchStyle, &raw, &observed, &item.Proof); err != nil {
			return nil, err
		}
		item.RawJSON = append(json.RawMessage(nil), raw...)
		item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, fmt.Errorf("parse filter observation time: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) GetLocationCache(ctx context.Context, queryKey string) (LocationCacheEntry, error) {
	var item LocationCacheEntry
	var raw []byte
	var observed, expires string
	err := d.sql.QueryRowContext(ctx, `SELECT query_key,result_json,observed_at,expires_at FROM location_cache WHERE query_key=?`, queryKey).Scan(&item.QueryKey, &raw, &observed, &expires)
	if err != nil {
		return item, err
	}
	item.ResultJSON = append(json.RawMessage(nil), raw...)
	item.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
	if err != nil {
		return item, fmt.Errorf("parse location observation time: %w", err)
	}
	item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return item, fmt.Errorf("parse location expiry time: %w", err)
	}
	return item, nil
}

func (d *DB) PutLocationCache(ctx context.Context, item LocationCacheEntry) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO location_cache(query_key,result_json,observed_at,expires_at) VALUES(?,?,?,?) ON CONFLICT(query_key) DO UPDATE SET result_json=excluded.result_json,observed_at=excluded.observed_at,expires_at=excluded.expires_at`, item.QueryKey, []byte(item.ResultJSON), item.ObservedAt.UTC().Format(time.RFC3339Nano), item.ExpiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
