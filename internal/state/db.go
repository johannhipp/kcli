package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/platform"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type DB struct {
	sql     *sql.DB
	queries *Queries
}

func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("state path must be a clean absolute path")
	}
	if err := platform.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	busyTimeout := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		busyTimeout = min(busyTimeout, time.Until(deadline))
		if busyTimeout <= 0 {
			return nil, context.DeadlineExceeded
		}
	}
	// SQLite's busy handler can outlive context cancellation. Bound its wait by
	// the remaining command budget as well, rounding up to whole milliseconds.
	busyMS := (busyTimeout + time.Millisecond - 1) / time.Millisecond
	dsn := u.String() + fmt.Sprintf("?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_pragma=synchronous(FULL)&_txlock=immediate", busyMS)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("open state database: %w", err)
	}
	db := &DB{sql: sqldb, queries: NewQueries(sqldb)}
	if err := db.migrate(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("protect state database: %w", err)
	}
	if err := db.ensureProfileUUID(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	if err := db.RecoverStaleExecuting(ctx, time.Now().UTC()); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) migrate(ctx context.Context) error {
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.sql, migrations, goose.WithSlog(logger))
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate state database: %w", err)
	}
	return nil
}

func (d *DB) ensureProfileUUID(ctx context.Context) error {
	_, err := d.queries.GetMeta(ctx, "profile_uuid")
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("read profile UUID: %w", err)
	}
	id, err := newUUID()
	if err != nil {
		return err
	}
	if err := d.queries.SetMeta(ctx, "profile_uuid", id); err != nil {
		return fmt.Errorf("store profile UUID: %w", err)
	}
	if err := d.queries.SetMeta(ctx, "cursor_generation", "0"); err != nil {
		return fmt.Errorf("store cursor generation: %w", err)
	}
	return nil
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate profile UUID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (d *DB) Close() error      { return d.sql.Close() }
func (d *DB) SQL() *sql.DB      { return d.sql }
func (d *DB) Queries() *Queries { return d.queries }

func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx, *Queries) error) error {
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := fn(tx, d.queries.WithTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *DB) AcquireLease(ctx context.Context, name, owner string, ttl time.Duration) (bool, error) {
	if err := validateLeasePart(name); err != nil {
		return false, err
	}
	if err := validateLeasePart(owner); err != nil {
		return false, err
	}
	if ttl <= 0 || ttl > time.Hour {
		return false, fmt.Errorf("lease TTL must be between zero and one hour")
	}
	now := time.Now().UnixMilli()
	expires := now + ttl.Milliseconds()
	var acquired bool
	err := d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO leases(name, owner, expires_at_ms) VALUES(?, ?, ?) ON CONFLICT(name) DO UPDATE SET owner=excluded.owner, expires_at_ms=excluded.expires_at_ms WHERE leases.expires_at_ms <= ? OR leases.owner = ?`, name, owner, expires, now, owner)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		acquired = rows == 1
		return nil
	})
	return acquired, err
}
func (d *DB) ReleaseLease(ctx context.Context, name, owner string) error {
	if err := validateLeasePart(name); err != nil {
		return err
	}
	if err := validateLeasePart(owner); err != nil {
		return err
	}
	return d.queries.DeleteLease(ctx, name, owner)
}
func validateLeasePart(value string) error {
	if value == "" || len(value) > 128 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("invalid lease identifier")
	}
	return nil
}
