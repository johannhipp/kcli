package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfirmationRecoveryRunsOnOpen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	profileUUID, err := database.ProfileUUID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour)
	plan, err := database.CreatePlan(ctx, CreatePlanInput{
		ProfileUUID: profileUUID, AccountHash: strings.Repeat("c", 64), OperationKind: ConfirmationKindReply,
		TargetID: "reply:v1:conv-process-death", MessageDigest: strings.Repeat("d", 64), Now: old,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimPlan(ctx, ClaimPlanInput{
		ConfirmationID: plan.ConfirmationID, ProfileUUID: plan.ProfileUUID,
		AccountHash: plan.AccountHash, OperationKind: plan.OperationKind, TargetID: plan.TargetID,
		MessageDigest: plan.MessageDigest, InitialStage: ConfirmationStageExecutingSend, Now: old.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	recovered, err := database.ConfirmationPlan(ctx, plan.ConfirmationID)
	if err != nil || recovered.State != ConfirmationStateOutcomeUnknown || recovered.Stage != ConfirmationStateOutcomeUnknown {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
}
