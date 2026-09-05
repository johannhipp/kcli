package state

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
)

const (
	authAccountHashKey      = "auth_account_hash"
	authAccountIDKey        = "auth_account_id"
	authLoginRequiredKey    = "auth_login_required"
	authCursorGenerationKey = "cursor_generation"
)

var (
	authAccountHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	authAccountIDPattern   = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)
)

type AuthAccountState struct {
	SubjectHash   string
	AccountID     string
	LoginRequired bool
}

type AuthLogoutEffect struct {
	HadAccount     bool
	AccountID      string
	NextGeneration int64
}

func (d *DB) AuthAccount(ctx context.Context) (AuthAccountState, bool, error) {
	var result AuthAccountState
	hash, err := d.queries.GetMeta(ctx, authAccountHashKey)
	if err == sql.ErrNoRows {
		return result, false, nil
	}
	if err != nil {
		return result, false, fmt.Errorf("read authenticated account hash: %w", err)
	}
	id, err := d.queries.GetMeta(ctx, authAccountIDKey)
	if err != nil {
		return result, false, fmt.Errorf("read authenticated account ID: %w", err)
	}
	if !authAccountHashPattern.MatchString(hash) || !authAccountIDPattern.MatchString(id) {
		return result, false, fmt.Errorf("stored authenticated account identity is invalid")
	}
	loginRequired, err := d.queries.GetMeta(ctx, authLoginRequiredKey)
	if err != nil && err != sql.ErrNoRows {
		return result, false, fmt.Errorf("read login-required state: %w", err)
	}
	if err == nil && loginRequired != "0" && loginRequired != "1" {
		return result, false, fmt.Errorf("stored login-required state is invalid")
	}
	result = AuthAccountState{SubjectHash: hash, AccountID: id, LoginRequired: loginRequired == "1"}
	return result, true, nil
}

func (d *DB) AuthStoreAccount(ctx context.Context, subjectHash, accountID string) error {
	if !authAccountHashPattern.MatchString(subjectHash) || !authAccountIDPattern.MatchString(accountID) {
		return fmt.Errorf("invalid authenticated account identity")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, q *Queries) error {
		previous, err := q.GetMeta(ctx, authAccountHashKey)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && previous != subjectHash {
			if err := authDeleteAccountData(ctx, tx, previous); err != nil {
				return err
			}
			if _, err := authAdvanceGeneration(ctx, q); err != nil {
				return err
			}
		}
		if err := q.SetMeta(ctx, authAccountHashKey, subjectHash); err != nil {
			return err
		}
		if err := q.SetMeta(ctx, authAccountIDKey, accountID); err != nil {
			return err
		}
		return q.SetMeta(ctx, authLoginRequiredKey, "0")
	})
}

func (d *DB) AuthSetLoginRequired(ctx context.Context) error {
	return d.queries.SetMeta(ctx, authLoginRequiredKey, "1")
}

func (d *DB) AuthLogoutPreview(ctx context.Context) (AuthLogoutEffect, error) {
	account, exists, err := d.AuthAccount(ctx)
	if err != nil {
		return AuthLogoutEffect{}, err
	}
	generation, err := d.queries.GetMeta(ctx, authCursorGenerationKey)
	if err != nil {
		return AuthLogoutEffect{}, fmt.Errorf("read cursor generation: %w", err)
	}
	current, err := strconv.ParseInt(generation, 10, 64)
	if err != nil || current < 0 || current == int64(^uint64(0)>>1) {
		return AuthLogoutEffect{}, fmt.Errorf("stored cursor generation is invalid")
	}
	return AuthLogoutEffect{HadAccount: exists, AccountID: account.AccountID, NextGeneration: current + 1}, nil
}

func (d *DB) AuthLogout(ctx context.Context) (AuthLogoutEffect, error) {
	var effect AuthLogoutEffect
	err := d.WithTx(ctx, func(tx *sql.Tx, q *Queries) error {
		hash, err := q.GetMeta(ctx, authAccountHashKey)
		if err == nil {
			id, idErr := q.GetMeta(ctx, authAccountIDKey)
			if idErr != nil && idErr != sql.ErrNoRows {
				return idErr
			}
			effect.HadAccount = true
			effect.AccountID = id
			if err := authDeleteAccountData(ctx, tx, hash); err != nil {
				return err
			}
		} else if err != sql.ErrNoRows {
			return err
		}
		generation, err := authAdvanceGeneration(ctx, q)
		if err != nil {
			return err
		}
		effect.NextGeneration = generation
		for _, key := range []string{authAccountHashKey, authAccountIDKey, authLoginRequiredKey} {
			if err := q.DeleteMeta(ctx, key); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return AuthLogoutEffect{}, fmt.Errorf("clear authenticated account state: %w", err)
	}
	return effect, nil
}

func authAdvanceGeneration(ctx context.Context, q *Queries) (int64, error) {
	stored, err := q.GetMeta(ctx, authCursorGenerationKey)
	if err != nil {
		return 0, err
	}
	generation, err := strconv.ParseInt(stored, 10, 64)
	if err != nil || generation < 0 || generation == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("stored cursor generation is invalid")
	}
	generation++
	if err := q.SetMeta(ctx, authCursorGenerationKey, strconv.FormatInt(generation, 10)); err != nil {
		return 0, err
	}
	return generation, nil
}

func authDeleteAccountData(ctx context.Context, tx *sql.Tx, accountHash string) error {
	if !authAccountHashPattern.MatchString(accountHash) {
		return fmt.Errorf("stored authenticated account hash is invalid")
	}
	for _, statement := range []string{
		`DELETE FROM confirmation_plans WHERE account_hash = ?`,
		`DELETE FROM conversations WHERE account_hash = ?`,
		`DELETE FROM events WHERE account_hash = ?`,
		`DELETE FROM cursor_heads WHERE account_hash = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, accountHash); err != nil {
			return err
		}
	}
	return nil
}
