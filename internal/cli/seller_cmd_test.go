package cli

import (
	"encoding/json"
	"testing"
)

func TestSellerCommandsRunWithExplicitScopeLabels(t *testing.T) {
	runtime, stdout, database := listingCLIRuntime(t)
	defer database.Close()
	if err := (&SellerGetCmd{Listing: "1234567890"}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["source"] != "listing" || envelope["completeness"] != "direct" || envelope["observed_at"] == nil {
		t.Fatalf("seller get output=%s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	if err := (&SellerSearchCmd{Name: "händler", Match: "contains"}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["source"] != "local-index" || envelope["completeness"] != "best-effort" {
		t.Fatalf("seller search output=%s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	if err := (&SellerListingsCmd{IDOrURL: "987654321", Limit: 25}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["source"] != "local-index" || envelope["completeness"] != "known-only" {
		t.Fatalf("seller listings output=%s err=%v", stdout.String(), err)
	}
}
