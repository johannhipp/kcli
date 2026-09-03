package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfirmationConversationOutcomeBlocksCrossOperationDuplicate(t *testing.T) {
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
	now := time.Now().UTC()
	accountHash := strings.Repeat("e", 64)
	digest := strings.Repeat("f", 64)
	createExecutingStart := func(target string) ConfirmationPlan {
		plan, createErr := database.CreatePlan(ctx, CreatePlanInput{
			ProfileUUID: profileUUID, AccountHash: accountHash, OperationKind: ConfirmationKindStart,
			TargetID: target, MessageDigest: digest, Now: now,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, claimErr := database.ClaimPlan(ctx, ClaimPlanInput{
			ConfirmationID: plan.ConfirmationID, ProfileUUID: profileUUID, AccountHash: accountHash,
			OperationKind: ConfirmationKindStart, TargetID: target, MessageDigest: digest,
			InitialStage: ConfirmationStageExecutingCreate, Now: now,
		}); claimErr != nil {
			t.Fatal(claimErr)
		}
		if transitionErr := database.MarkConversationCreated(ctx, plan.ConfirmationID, "conv-cross", now); transitionErr != nil {
			t.Fatal(transitionErr)
		}
		if transitionErr := database.MarkExecutingSend(ctx, plan.ConfirmationID, now); transitionErr != nil {
			t.Fatal(transitionErr)
		}
		return plan
	}
	unknown := createExecutingStart("start:v1:one:" + strings.Repeat("1", 64))
	if err := database.MarkOutcomeUnknownForConversation(ctx, unknown.ConfirmationID, "conv-cross", now); err != nil {
		t.Fatal(err)
	}
	found, err := database.FindUnresolvedConversation(ctx, accountHash, "conv-cross", digest)
	if err != nil || found == nil || found.ConfirmationID != unknown.ConfirmationID || CreatedConversationID(*found) != "conv-cross" {
		t.Fatalf("unresolved=%#v err=%v", found, err)
	}

	warning := createExecutingStart("start:v1:two:" + strings.Repeat("2", 64))
	if err := database.MarkWarningBlockedForConversation(ctx, warning.ConfirmationID, "warnEmail", "conv-cross", now); err != nil {
		t.Fatal(err)
	}
	blocked, err := database.FindWarningBlockedConversation(ctx, accountHash, "conv-cross", digest)
	if err != nil || blocked == nil || blocked.ConfirmationID != warning.ConfirmationID || WarningCodeFromPlan(*blocked) != "warnEmail" || CreatedConversationID(*blocked) != "conv-cross" {
		t.Fatalf("blocked=%#v err=%v", blocked, err)
	}
}
