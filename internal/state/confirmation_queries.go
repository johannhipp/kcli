package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ConfirmationKindReply = "reply"
	ConfirmationKindStart = "start"

	ConfirmationStatePlanned        = "planned"
	ConfirmationStateExecuting      = "executing"
	ConfirmationStateSent           = "sent"
	ConfirmationStateFailedDefinite = "failed_definite"
	ConfirmationStateWarningBlocked = "warning_blocked"
	ConfirmationStateOutcomeUnknown = "outcome_unknown"
	ConfirmationStateExpired        = "expired"

	ConfirmationStagePlanned             = "planned"
	ConfirmationStageExecutingSend       = "executing_send"
	ConfirmationStageExecutingCreate     = "executing_create"
	ConfirmationStageConversationCreated = "conversation_created"
	ConfirmationStageReconcileCreate     = "reconcile_create"
)

const (
	confirmationLifetime          = 10 * time.Minute
	confirmationExecutionDeadline = 30 * time.Second
)

var (
	ErrConfirmationNotFound       = errors.New("confirmation plan was not found")
	ErrConfirmationExpired        = errors.New("confirmation plan expired")
	ErrConfirmationMismatch       = errors.New("confirmation plan bindings do not match")
	ErrConfirmationAlreadyUsed    = errors.New("confirmation plan was already claimed")
	confirmationIDPattern         = regexp.MustCompile(`^cnf_[A-Za-z0-9_-]{32}$`)
	confirmationUUIDPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	confirmationDigestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	confirmationWarningPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	confirmationIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type ConfirmationPlan struct {
	ConfirmationID string
	ProfileUUID    string
	AccountHash    string
	OperationKind  string
	TargetID       string
	MessageDigest  string
	State          string
	Stage          string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Outcome        string
}

type CreatePlanInput struct {
	ProfileUUID   string
	AccountHash   string
	OperationKind string
	TargetID      string
	MessageDigest string
	Now           time.Time
}

type ClaimPlanInput struct {
	ConfirmationID string
	ProfileUUID    string
	AccountHash    string
	OperationKind  string
	TargetID       string
	MessageDigest  string
	InitialStage   string
	Now            time.Time
}

// ProfileUUID returns the random, stable identifier bound to this profile's
// private state database.
func (d *DB) ProfileUUID(ctx context.Context) (string, error) {
	if d == nil || d.queries == nil {
		return "", fmt.Errorf("state database is unavailable")
	}
	value, err := d.queries.GetMeta(ctx, "profile_uuid")
	if err != nil {
		return "", fmt.Errorf("read profile UUID: %w", err)
	}
	if !confirmationUUIDPattern.MatchString(value) {
		return "", fmt.Errorf("stored profile UUID is invalid")
	}
	return value, nil
}

// CreatePlan stores only confirmation bindings and a message digest. The
// message body is deliberately not accepted by this API.
func (d *DB) CreatePlan(ctx context.Context, input CreatePlanInput) (ConfirmationPlan, error) {
	if d == nil || d.sql == nil {
		return ConfirmationPlan{}, fmt.Errorf("state database is unavailable")
	}
	if err := validateCreatePlan(input); err != nil {
		return ConfirmationPlan{}, err
	}
	id, err := newConfirmationID()
	if err != nil {
		return ConfirmationPlan{}, err
	}
	now := input.Now.UTC()
	plan := ConfirmationPlan{
		ConfirmationID: id,
		ProfileUUID:    input.ProfileUUID,
		AccountHash:    input.AccountHash,
		OperationKind:  input.OperationKind,
		TargetID:       input.TargetID,
		MessageDigest:  input.MessageDigest,
		State:          ConfirmationStatePlanned,
		Stage:          ConfirmationStagePlanned,
		CreatedAt:      now,
		ExpiresAt:      now.Add(confirmationLifetime),
	}
	_, err = d.sql.ExecContext(ctx, `INSERT INTO confirmation_plans(
		confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
		message_digest, state, stage, created_at, expires_at, outcome
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		plan.ConfirmationID, plan.ProfileUUID, plan.AccountHash, plan.OperationKind,
		plan.TargetID, plan.MessageDigest, plan.State, plan.Stage,
		formatConfirmationTime(plan.CreatedAt), formatConfirmationTime(plan.ExpiresAt))
	if err != nil {
		return ConfirmationPlan{}, fmt.Errorf("create confirmation plan: %w", err)
	}
	return plan, nil
}

// ClaimPlan compares every stored binding and atomically consumes a planned
// confirmation. A plan never returns to planned after this transition.
func (d *DB) ClaimPlan(ctx context.Context, input ClaimPlanInput) (ConfirmationPlan, error) {
	if d == nil || d.sql == nil {
		return ConfirmationPlan{}, fmt.Errorf("state database is unavailable")
	}
	if err := validateClaimPlan(input); err != nil {
		return ConfirmationPlan{}, err
	}
	var claimed ConfirmationPlan
	var claimErr error
	err := d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		plan, err := scanConfirmationPlan(tx.QueryRowContext(ctx, `SELECT
			confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
			message_digest, state, stage, created_at, expires_at, outcome
			FROM confirmation_plans WHERE confirmation_id = ?`, input.ConfirmationID))
		if errors.Is(err, sql.ErrNoRows) {
			claimErr = ErrConfirmationNotFound
			return nil
		}
		if err != nil {
			return err
		}
		claimed = plan
		if plan.ProfileUUID != input.ProfileUUID || plan.AccountHash != input.AccountHash || plan.OperationKind != input.OperationKind || plan.TargetID != input.TargetID || plan.MessageDigest != input.MessageDigest {
			claimErr = ErrConfirmationMismatch
			return nil
		}
		if plan.State != ConfirmationStatePlanned || plan.Stage != ConfirmationStagePlanned {
			claimErr = ErrConfirmationAlreadyUsed
			return nil
		}
		if !input.Now.UTC().Before(plan.ExpiresAt) {
			result, err := tx.ExecContext(ctx, `UPDATE confirmation_plans SET state = ?, stage = ?, outcome = ? WHERE confirmation_id = ? AND state = ? AND stage = ?`, ConfirmationStateExpired, ConfirmationStateExpired, "expired", plan.ConfirmationID, ConfirmationStatePlanned, ConfirmationStagePlanned)
			if err != nil {
				return err
			}
			if rows, err := result.RowsAffected(); err != nil || rows != 1 {
				if err != nil {
					return err
				}
				return fmt.Errorf("expire confirmation plan: concurrent transition")
			}
			claimed.State, claimed.Stage, claimed.Outcome = ConfirmationStateExpired, ConfirmationStateExpired, "expired"
			claimErr = ErrConfirmationExpired
			return nil
		}
		executionDeadline := input.Now.UTC().Add(confirmationExecutionDeadline)
		result, err := tx.ExecContext(ctx, `UPDATE confirmation_plans SET state = ?, stage = ?, outcome = NULL, expires_at = ? WHERE confirmation_id = ? AND profile_uuid = ? AND account_hash = ? AND operation_kind = ? AND target_id = ? AND message_digest = ? AND state = ? AND stage = ?`,
			ConfirmationStateExecuting, input.InitialStage, formatConfirmationTime(executionDeadline),
			plan.ConfirmationID, input.ProfileUUID, input.AccountHash, input.OperationKind,
			input.TargetID, input.MessageDigest, ConfirmationStatePlanned,
			ConfirmationStagePlanned)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			claimErr = ErrConfirmationAlreadyUsed
			return nil
		}
		claimed.State, claimed.Stage, claimed.Outcome, claimed.ExpiresAt = ConfirmationStateExecuting, input.InitialStage, "", executionDeadline
		return nil
	})
	if err != nil {
		return ConfirmationPlan{}, fmt.Errorf("claim confirmation plan: %w", err)
	}
	if claimErr != nil {
		return claimed, claimErr
	}
	return claimed, nil
}

func (d *DB) MarkSent(ctx context.Context, confirmationID, outcome string, now time.Time) error {
	if outcome != "sent" && outcome != "sent_reconciled" {
		return fmt.Errorf("invalid sent outcome")
	}
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateSent, ConfirmationStateSent, outcome, []string{ConfirmationStageExecutingSend})
}

func (d *DB) MarkFailedDefinite(ctx context.Context, confirmationID string, now time.Time) error {
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateFailedDefinite, ConfirmationStateFailedDefinite, "failed_definite", []string{ConfirmationStageExecutingCreate, ConfirmationStageReconcileCreate, ConfirmationStageConversationCreated, ConfirmationStageExecutingSend})
}

func (d *DB) MarkWarningBlocked(ctx context.Context, confirmationID, warningCode string, now time.Time) error {
	if !confirmationWarningPattern.MatchString(warningCode) {
		return fmt.Errorf("invalid platform warning code")
	}
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateWarningBlocked, ConfirmationStateWarningBlocked, "warning:"+warningCode, []string{ConfirmationStageExecutingSend})
}

func (d *DB) MarkWarningBlockedForConversation(ctx context.Context, confirmationID, warningCode, conversationID string, now time.Time) error {
	if !confirmationWarningPattern.MatchString(warningCode) || !confirmationIdentifierPattern.MatchString(conversationID) {
		return fmt.Errorf("invalid platform warning metadata")
	}
	outcome := "warning:" + warningCode + ":conversation:" + conversationID
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateWarningBlocked, ConfirmationStateWarningBlocked, outcome, []string{ConfirmationStageExecutingSend})
}

func (d *DB) MarkOutcomeUnknown(ctx context.Context, confirmationID string, now time.Time) error {
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateOutcomeUnknown, ConfirmationStateOutcomeUnknown, "outcome_unknown", []string{ConfirmationStageExecutingCreate, ConfirmationStageReconcileCreate, ConfirmationStageConversationCreated, ConfirmationStageExecutingSend})
}

func (d *DB) MarkOutcomeUnknownForConversation(ctx context.Context, confirmationID, conversationID string, now time.Time) error {
	if !confirmationIdentifierPattern.MatchString(conversationID) {
		return fmt.Errorf("invalid ambiguous conversation ID")
	}
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateOutcomeUnknown, ConfirmationStateOutcomeUnknown, "outcome_unknown:conversation:"+conversationID, []string{ConfirmationStageConversationCreated, ConfirmationStageExecutingSend})
}

func (d *DB) MarkExpired(ctx context.Context, confirmationID string, now time.Time) error {
	if d == nil || d.sql == nil || !confirmationIDPattern.MatchString(confirmationID) {
		return ErrConfirmationNotFound
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE confirmation_plans SET state = ?, stage = ?, outcome = ? WHERE confirmation_id = ? AND state = ? AND stage = ?`, ConfirmationStateExpired, ConfirmationStateExpired, "expired", confirmationID, ConfirmationStatePlanned, ConfirmationStagePlanned)
	if err != nil {
		return fmt.Errorf("expire confirmation plan: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConfirmationAlreadyUsed
	}
	return nil
}

func (d *DB) MarkConversationCreated(ctx context.Context, confirmationID, conversationID string, now time.Time) error {
	if !confirmationIdentifierPattern.MatchString(conversationID) {
		return fmt.Errorf("invalid created conversation ID")
	}
	return d.transitionPlan(ctx, confirmationID, ConfirmationStateExecuting, ConfirmationStageConversationCreated, "conversation:"+conversationID, []string{ConfirmationStageExecutingCreate, ConfirmationStageReconcileCreate})
}

func (d *DB) MarkExecutingSend(ctx context.Context, confirmationID string, now time.Time) error {
	return d.transitionPlanPreservingOutcome(ctx, confirmationID, ConfirmationStageExecutingSend, now, []string{ConfirmationStageConversationCreated})
}

func (d *DB) MarkReconcileCreate(ctx context.Context, confirmationID string, now time.Time) error {
	return d.transitionPlanPreservingOutcome(ctx, confirmationID, ConfirmationStageReconcileCreate, now, []string{ConfirmationStageExecutingCreate})
}

// RecoverStaleExecuting makes every past-deadline in-flight operation
// terminally ambiguous. A conversation_created plan is also in-flight: a
// process may have died in the narrow gap before recording executing_send.
func (d *DB) RecoverStaleExecuting(ctx context.Context, now time.Time) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("state database is unavailable")
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE confirmation_plans
		SET state = ?, stage = ?,
		    outcome = CASE WHEN outcome LIKE 'conversation:%'
		                   THEN 'outcome_unknown:' || outcome
		                   ELSE 'outcome_unknown' END
		WHERE state = ? AND stage IN (?, ?, ?, ?) AND julianday(expires_at) <= julianday(?)`,
		ConfirmationStateOutcomeUnknown, ConfirmationStateOutcomeUnknown,
		ConfirmationStateExecuting, ConfirmationStageExecutingCreate, ConfirmationStageExecutingSend,
		ConfirmationStageReconcileCreate, ConfirmationStageConversationCreated, formatConfirmationTime(now.UTC()))
	if err != nil {
		return fmt.Errorf("recover stale confirmation plans: %w", err)
	}
	return nil
}

// FindUnresolved returns an outcome-unknown plan for the exact account,
// operation, target, and body digest.
func (d *DB) FindUnresolved(ctx context.Context, accountHash, operationKind, targetID, messageDigest string) (*ConfirmationPlan, error) {
	return d.findLatestPlan(ctx, accountHash, operationKind, targetID, messageDigest, ConfirmationStateOutcomeUnknown)
}

func (d *DB) FindWarningBlocked(ctx context.Context, accountHash, operationKind, targetID, messageDigest string) (*ConfirmationPlan, error) {
	return d.findLatestPlan(ctx, accountHash, operationKind, targetID, messageDigest, ConfirmationStateWarningBlocked)
}

func (d *DB) FindUnresolvedConversation(ctx context.Context, accountHash, conversationID, messageDigest string) (*ConfirmationPlan, error) {
	return d.findLatestConversationOutcome(ctx, accountHash, conversationID, messageDigest, ConfirmationStateOutcomeUnknown, "outcome_unknown:conversation:"+conversationID)
}

func (d *DB) FindWarningBlockedConversation(ctx context.Context, accountHash, conversationID, messageDigest string) (*ConfirmationPlan, error) {
	return d.findLatestConversationOutcome(ctx, accountHash, conversationID, messageDigest, ConfirmationStateWarningBlocked, "")
}

func (d *DB) ConfirmationPlan(ctx context.Context, confirmationID string) (ConfirmationPlan, error) {
	if d == nil || d.sql == nil || !confirmationIDPattern.MatchString(confirmationID) {
		return ConfirmationPlan{}, ErrConfirmationNotFound
	}
	plan, err := scanConfirmationPlan(d.sql.QueryRowContext(ctx, `SELECT
		confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
		message_digest, state, stage, created_at, expires_at, outcome
		FROM confirmation_plans WHERE confirmation_id = ?`, confirmationID))
	if errors.Is(err, sql.ErrNoRows) {
		return ConfirmationPlan{}, ErrConfirmationNotFound
	}
	return plan, err
}

func (d *DB) transitionPlan(ctx context.Context, confirmationID, nextState, nextStage, outcome string, allowedStages []string) error {
	if d == nil || d.sql == nil || !confirmationIDPattern.MatchString(confirmationID) {
		return ErrConfirmationNotFound
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(allowedStages)), ",")
	arguments := []any{nextState, nextStage, outcome, confirmationID, ConfirmationStateExecuting}
	for _, stage := range allowedStages {
		arguments = append(arguments, stage)
	}
	query := `UPDATE confirmation_plans SET state = ?, stage = ?, outcome = ? WHERE confirmation_id = ? AND state = ? AND stage IN (` + placeholders + `)`
	result, err := d.sql.ExecContext(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("transition confirmation plan: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConfirmationAlreadyUsed
	}
	return nil
}

func (d *DB) transitionPlanPreservingOutcome(ctx context.Context, confirmationID, nextStage string, now time.Time, allowedStages []string) error {
	if d == nil || d.sql == nil || !confirmationIDPattern.MatchString(confirmationID) {
		return ErrConfirmationNotFound
	}
	if now.IsZero() {
		return fmt.Errorf("confirmation stage time is required")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(allowedStages)), ",")
	arguments := []any{nextStage, formatConfirmationTime(now.UTC().Add(confirmationExecutionDeadline)), confirmationID, ConfirmationStateExecuting}
	for _, stage := range allowedStages {
		arguments = append(arguments, stage)
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE confirmation_plans SET stage = ?, expires_at = ? WHERE confirmation_id = ? AND state = ? AND stage IN (`+placeholders+`)`, arguments...)
	if err != nil {
		return fmt.Errorf("advance confirmation stage: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConfirmationAlreadyUsed
	}
	return nil
}

func (d *DB) findLatestPlan(ctx context.Context, accountHash, operationKind, targetID, messageDigest, planState string) (*ConfirmationPlan, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("state database is unavailable")
	}
	if !authAccountHashPattern.MatchString(accountHash) || !validConfirmationOperation(operationKind) || !validConfirmationTarget(targetID) || !confirmationDigestPattern.MatchString(messageDigest) {
		return nil, fmt.Errorf("invalid confirmation lookup")
	}
	plan, err := scanConfirmationPlan(d.sql.QueryRowContext(ctx, `SELECT
		confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
		message_digest, state, stage, created_at, expires_at, outcome
		FROM confirmation_plans
		WHERE account_hash = ? AND operation_kind = ? AND target_id = ? AND message_digest = ? AND state = ?
		ORDER BY created_at DESC, confirmation_id DESC LIMIT 1`, accountHash, operationKind, targetID, messageDigest, planState))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find confirmation plan: %w", err)
	}
	return &plan, nil
}

func (d *DB) findLatestConversationOutcome(ctx context.Context, accountHash, conversationID, messageDigest, planState, exactOutcome string) (*ConfirmationPlan, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("state database is unavailable")
	}
	if !authAccountHashPattern.MatchString(accountHash) || !confirmationIdentifierPattern.MatchString(conversationID) || !confirmationDigestPattern.MatchString(messageDigest) {
		return nil, fmt.Errorf("invalid conversation confirmation lookup")
	}
	query := `SELECT confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
		message_digest, state, stage, created_at, expires_at, outcome
		FROM confirmation_plans
		WHERE account_hash = ? AND message_digest = ? AND state = ? AND outcome = ?
		ORDER BY created_at DESC, confirmation_id DESC LIMIT 1`
	arguments := []any{accountHash, messageDigest, planState, exactOutcome}
	if exactOutcome == "" {
		suffix := ":conversation:" + conversationID
		query = `SELECT confirmation_id, profile_uuid, account_hash, operation_kind, target_id,
			message_digest, state, stage, created_at, expires_at, outcome
			FROM confirmation_plans
			WHERE account_hash = ? AND message_digest = ? AND state = ?
			  AND substr(outcome, -length(?)) = ?
			ORDER BY created_at DESC, confirmation_id DESC LIMIT 1`
		arguments = []any{accountHash, messageDigest, planState, suffix, suffix}
	}
	plan, err := scanConfirmationPlan(d.sql.QueryRowContext(ctx, query, arguments...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find conversation confirmation plan: %w", err)
	}
	return &plan, nil
}

type confirmationScanner interface {
	Scan(...any) error
}

func scanConfirmationPlan(row confirmationScanner) (ConfirmationPlan, error) {
	var plan ConfirmationPlan
	var created, expires string
	var outcome sql.NullString
	if err := row.Scan(&plan.ConfirmationID, &plan.ProfileUUID, &plan.AccountHash,
		&plan.OperationKind, &plan.TargetID, &plan.MessageDigest, &plan.State,
		&plan.Stage, &created, &expires, &outcome); err != nil {
		return ConfirmationPlan{}, err
	}
	var err error
	plan.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return ConfirmationPlan{}, fmt.Errorf("invalid confirmation creation time")
	}
	plan.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return ConfirmationPlan{}, fmt.Errorf("invalid confirmation expiry time")
	}
	if outcome.Valid {
		plan.Outcome = outcome.String
	}
	return plan, nil
}

func validateCreatePlan(input CreatePlanInput) error {
	if !confirmationUUIDPattern.MatchString(input.ProfileUUID) || !authAccountHashPattern.MatchString(input.AccountHash) || !validConfirmationOperation(input.OperationKind) || !validConfirmationTarget(input.TargetID) || !confirmationDigestPattern.MatchString(input.MessageDigest) || input.Now.IsZero() {
		return fmt.Errorf("invalid confirmation plan bindings")
	}
	return nil
}

func validateClaimPlan(input ClaimPlanInput) error {
	if !confirmationIDPattern.MatchString(input.ConfirmationID) || input.Now.IsZero() {
		return ErrConfirmationMismatch
	}
	if err := validateCreatePlan(CreatePlanInput{ProfileUUID: input.ProfileUUID, AccountHash: input.AccountHash, OperationKind: input.OperationKind, TargetID: input.TargetID, MessageDigest: input.MessageDigest, Now: input.Now}); err != nil {
		return ErrConfirmationMismatch
	}
	if (input.OperationKind == ConfirmationKindReply && input.InitialStage != ConfirmationStageExecutingSend) || (input.OperationKind == ConfirmationKindStart && input.InitialStage != ConfirmationStageExecutingCreate) {
		return ErrConfirmationMismatch
	}
	return nil
}

func validConfirmationOperation(value string) bool {
	return value == ConfirmationKindReply || value == ConfirmationKindStart
}

func validConfirmationTarget(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.ContainsAny(value, "\x00\r\n")
}

func newConfirmationID() (string, error) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate confirmation ID: %w", err)
	}
	return "cnf_" + base64.RawURLEncoding.EncodeToString(random[:]), nil
}

func formatConfirmationTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

func WarningCodeFromPlan(plan ConfirmationPlan) string {
	const prefix = "warning:"
	if !strings.HasPrefix(plan.Outcome, prefix) {
		return ""
	}
	code := strings.TrimPrefix(plan.Outcome, prefix)
	if before, _, found := strings.Cut(code, ":conversation:"); found {
		code = before
	}
	if confirmationWarningPattern.MatchString(code) {
		return code
	}
	return ""
}

func CreatedConversationID(plan ConfirmationPlan) string {
	const prefix = "conversation:"
	if strings.HasPrefix(plan.Outcome, prefix) {
		id := strings.TrimPrefix(plan.Outcome, prefix)
		if confirmationIdentifierPattern.MatchString(id) {
			return id
		}
	}
	if _, id, found := strings.Cut(plan.Outcome, ":conversation:"); found && confirmationIdentifierPattern.MatchString(id) {
		return id
	}
	return ""
}
