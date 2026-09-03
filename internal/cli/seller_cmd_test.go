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
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["account_type"] != "COMMERCIAL" || data["account_since"] == nil || data["rating"] == nil || data["badges"] == nil || data["company"] == nil || data["public"] == nil || data["source"] != "listing" || data["completeness"] != "direct" || data["observed_at"] == nil {
		t.Fatalf("seller get data is incomplete: %#v", envelope["data"])
	}
	stdout.Reset()
	if err := (&SellerSearchCmd{Name: "händler", Match: "contains"}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["source"] != "local-index" || envelope["completeness"] != "best-effort" {
		t.Fatalf("seller search output=%s err=%v", stdout.String(), err)
	}
	results, ok := envelope["data"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("seller search data=%#v", envelope["data"])
	}
	seller, ok := results[0].(map[string]any)
	if !ok || seller["rating"] == nil || seller["badges"] == nil || seller["account_type"] == nil || seller["company"] == nil || seller["observed_at"] == nil || seller["source"] != "local-index" || seller["completeness"] != "best-effort" {
		t.Fatalf("seller search item is incomplete: %#v", results[0])
	}
	stdout.Reset()
	if err := (&SellerListingsCmd{IDOrURL: "987654321", Limit: 25}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["source"] != "local-index" || envelope["completeness"] != "known-only" {
		t.Fatalf("seller listings output=%s err=%v", stdout.String(), err)
	}
	listings, ok := envelope["data"].([]any)
	if !ok || len(listings) != 1 {
		t.Fatalf("seller listings data=%#v", envelope["data"])
	}
	listing, ok := listings[0].(map[string]any)
	if !ok || listing["status"] != "available" || listing["source"] != "local-index" || listing["completeness"] != "known-only" || listing["observed_at"] == nil {
		t.Fatalf("seller listing item is incomplete: %#v", listings[0])
	}
}
