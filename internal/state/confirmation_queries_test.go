package state

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfirmationPlanDigestOnlyAtomicClaimExpiryAndRecovery(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	profileUUID, err := database.ProfileUUID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accountHash := strings.Repeat("a", 64)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	body := "[REDACTED_SENTINEL_NOT_FOR_STORAGE]"
	input := CreatePlanInput{
		ProfileUUID: profileUUID, AccountHash: accountHash, OperationKind: ConfirmationKindReply,
		TargetID: "reply:v1:conv-one", MessageDigest: DigestMessageContent(body), Now: now,
	}
	plan, err := database.CreatePlan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ConfirmationID == "" || plan.ExpiresAt.Sub(plan.CreatedAt) != 10*time.Minute || plan.Stage != ConfirmationStagePlanned {
		t.Fatalf("plan=%#v", plan)
	}
	var persisted string
	if err := database.SQL().QueryRowContext(ctx, `SELECT group_concat(confirmation_id || profile_uuid || account_hash || operation_kind || target_id || message_digest || state || stage || ifnull(outcome, '')) FROM confirmation_plans`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, body) || !strings.Contains(persisted, input.MessageDigest) {
		t.Fatalf("confirmation persistence leaked body or omitted digest: %q", persisted)
	}

	wrong := ClaimPlanInput{
		ConfirmationID: plan.ConfirmationID, ProfileUUID: profileUUID, AccountHash: accountHash,
		OperationKind: ConfirmationKindReply, TargetID: input.TargetID,
		MessageDigest: DigestMessageContent(body + " tampered"), InitialStage: ConfirmationStageExecutingSend,
		Now: now.Add(time.Minute),
	}
	if _, err := database.ClaimPlan(ctx, wrong); !errors.Is(err, ErrConfirmationMismatch) {
		t.Fatalf("tampered claim error=%v", err)
	}
	for label, mismatch := range map[string]ClaimPlanInput{
		"profile": {
			ConfirmationID: plan.ConfirmationID, ProfileUUID: "123e4567-e89b-42d3-a456-426614174000",
			AccountHash: accountHash, OperationKind: ConfirmationKindReply, TargetID: input.TargetID,
			MessageDigest: input.MessageDigest, InitialStage: ConfirmationStageExecutingSend, Now: now.Add(time.Minute),
		},
		"account": {
			ConfirmationID: plan.ConfirmationID, ProfileUUID: profileUUID,
			AccountHash: strings.Repeat("b", 64), OperationKind: ConfirmationKindReply, TargetID: input.TargetID,
			MessageDigest: input.MessageDigest, InitialStage: ConfirmationStageExecutingSend, Now: now.Add(time.Minute),
		},
		"target": {
			ConfirmationID: plan.ConfirmationID, ProfileUUID: profileUUID,
			AccountHash: accountHash, OperationKind: ConfirmationKindReply, TargetID: "reply:v1:conv-other",
			MessageDigest: input.MessageDigest, InitialStage: ConfirmationStageExecutingSend, Now: now.Add(time.Minute),
		},
		"kind": {
			ConfirmationID: plan.ConfirmationID, ProfileUUID: profileUUID,
			AccountHash: accountHash, OperationKind: ConfirmationKindStart, TargetID: input.TargetID,
			MessageDigest: input.MessageDigest, InitialStage: ConfirmationStageExecutingCreate, Now: now.Add(time.Minute),
		},
	} {
		if _, err := database.ClaimPlan(ctx, mismatch); !errors.Is(err, ErrConfirmationMismatch) {
			t.Fatalf("%s binding claim error=%v", label, err)
		}
	}

	claim := wrong
	claim.MessageDigest = input.MessageDigest
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := database.ClaimPlan(ctx, claim); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrConfirmationAlreadyUsed) {
				t.Errorf("concurrent claim error=%v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful claims=%d", successes.Load())
	}
	claimedPlan, err := database.ConfirmationPlan(ctx, plan.ConfirmationID)
	if err != nil || claimedPlan.ExpiresAt.Sub(claim.Now.UTC()) != confirmationExecutionDeadline {
		t.Fatalf("claimed execution deadline=%v err=%v", claimedPlan.ExpiresAt, err)
	}

	expiring, err := database.CreatePlan(ctx, CreatePlanInput{
		ProfileUUID: profileUUID, AccountHash: accountHash, OperationKind: ConfirmationKindReply,
		TargetID: "reply:v1:conv-expiring", MessageDigest: input.MessageDigest, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ClaimPlan(ctx, ClaimPlanInput{
		ConfirmationID: expiring.ConfirmationID, ProfileUUID: profileUUID, AccountHash: accountHash,
		OperationKind: ConfirmationKindReply, TargetID: "reply:v1:conv-expiring",
		MessageDigest: input.MessageDigest, InitialStage: ConfirmationStageExecutingSend,
		Now: now.Add(10 * time.Minute),
	})
	if !errors.Is(err, ErrConfirmationExpired) {
		t.Fatalf("expiry error=%v", err)
	}
	expired, err := database.ConfirmationPlan(ctx, expiring.ConfirmationID)
	if err != nil || expired.State != ConfirmationStateExpired {
		t.Fatalf("expired=%#v err=%v", expired, err)
	}

	stale, err := database.CreatePlan(ctx, CreatePlanInput{
		ProfileUUID: profileUUID, AccountHash: accountHash, OperationKind: ConfirmationKindStart,
		TargetID: "start:v1:123:" + strings.Repeat("b", 64), MessageDigest: input.MessageDigest, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimPlan(ctx, ClaimPlanInput{
		ConfirmationID: stale.ConfirmationID, ProfileUUID: profileUUID, AccountHash: accountHash,
		OperationKind: ConfirmationKindStart, TargetID: stale.TargetID, MessageDigest: stale.MessageDigest,
		InitialStage: ConfirmationStageExecutingCreate, Now: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecoverStaleExecuting(ctx, now.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovered, err := database.ConfirmationPlan(ctx, stale.ConfirmationID)
	if err != nil || recovered.State != ConfirmationStateOutcomeUnknown || recovered.Stage != ConfirmationStateOutcomeUnknown {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	if _, err := database.ClaimPlan(ctx, ClaimPlanInput{
		ConfirmationID: stale.ConfirmationID, ProfileUUID: profileUUID, AccountHash: accountHash,
		OperationKind: ConfirmationKindStart, TargetID: stale.TargetID, MessageDigest: stale.MessageDigest,
		InitialStage: ConfirmationStageExecutingCreate, Now: now.Add(11 * time.Minute),
	}); !errors.Is(err, ErrConfirmationAlreadyUsed) {
		t.Fatalf("recovered plan was claimable: %v", err)
	}
	unresolved, err := database.FindUnresolved(ctx, accountHash, ConfirmationKindStart, stale.TargetID, stale.MessageDigest)
	if err != nil || unresolved == nil || unresolved.ConfirmationID != stale.ConfirmationID {
		t.Fatalf("unresolved=%#v err=%v", unresolved, err)
	}
}
