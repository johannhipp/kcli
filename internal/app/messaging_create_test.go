package app

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

func TestDMStartAmbiguousCreateReconcilesUniqueConversationOnce(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 4, 0, 0, time.UTC)
	transport := &ambiguousCreateTransport{
		listing: dmAppFixture(t, "listing.json"),
		unique:  true,
	}
	application, database := messagingTestApp(t, transport, now)
	message := "[REDACTED_FIRST_AFTER_CREATE]"
	input := domain.DMStartInputV1{ListingIDOrURL: "1234567890", ContactName: "[REDACTED_CONTACT]", Message: message, DryRun: true}
	planned, err := application.DMStart(context.Background(), "default", "create-plan", input)
	if err != nil {
		t.Fatal(err)
	}
	confirmationID := planned.Data["confirmation_id"].(string)
	input.DryRun = false
	input.Confirm = confirmationID
	result, err := application.DMStart(context.Background(), "default", "create-confirm", input)
	if err != nil || result.Data["outcome"] != "sent" || result.Data["conversation_id"] != "conv-created" || transport.creates.Load() != 1 || transport.sends.Load() != 1 || transport.opens.Load() != 1 {
		t.Fatalf("result=%#v err=%v creates=%d sends=%d opens=%d", result.Data, err, transport.creates.Load(), transport.sends.Load(), transport.opens.Load())
	}
	stored, err := database.ConfirmationPlan(context.Background(), confirmationID)
	if err != nil || stored.State != state.ConfirmationStateSent || stored.Outcome != "sent" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	ambiguous := &ambiguousCreateTransport{listing: dmAppFixture(t, "listing.json"), unique: false}
	ambiguousApp, ambiguousDB := messagingTestApp(t, ambiguous, now)
	input.DryRun = true
	input.Confirm = ""
	ambiguousPlan, err := ambiguousApp.DMStart(context.Background(), "default", "ambiguous-create-plan", input)
	if err != nil {
		t.Fatal(err)
	}
	ambiguousID := ambiguousPlan.Data["confirmation_id"].(string)
	input.DryRun = false
	input.Confirm = ambiguousID
	_, err = ambiguousApp.DMStart(context.Background(), "default", "ambiguous-create-confirm", input)
	if domain.ExitCode(err) != 8 || ambiguous.creates.Load() != 1 || ambiguous.sends.Load() != 0 {
		t.Fatalf("err=%#v creates=%d sends=%d", err, ambiguous.creates.Load(), ambiguous.sends.Load())
	}
	unknown, stateErr := ambiguousDB.ConfirmationPlan(context.Background(), ambiguousID)
	if stateErr != nil || unknown.State != state.ConfirmationStateOutcomeUnknown {
		t.Fatalf("unknown=%#v stateErr=%v", unknown, stateErr)
	}
}

type ambiguousCreateTransport struct {
	listing   []byte
	unique    bool
	listCalls atomic.Int32
	creates   atomic.Int32
	sends     atomic.Int32
	opens     atomic.Int32
}

func (t *ambiguousCreateTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	switch {
	case request.Class == kleinanzeigen.ExternalCreate:
		t.creates.Add(1)
		return kleinanzeigen.Response{}, errors.New("create response lost after request write")
	case request.Class == kleinanzeigen.ExternalSend:
		t.sends.Add(1)
		return kleinanzeigen.Response{StatusCode: http.StatusNoContent}, nil
	case request.Host == kleinanzeigen.HostMain && request.Method == http.MethodGet:
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.listing...)}, nil
	case request.Method == http.MethodPut:
		t.opens.Add(1)
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":{"id":"conv-created","adId":"1234567890","messages":[]}}`)}, nil
	case request.Method == http.MethodGet:
		call := t.listCalls.Add(1)
		if call <= 2 {
			return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`)}, nil
		}
		if t.unique {
			return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"conv-created","adId":"1234567890","role":"BUYER","sellerName":"[REDACTED_SELLER]"}]}`)}, nil
		}
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"conv-created","adId":"1234567890"},{"id":"conv-other","adId":"1234567890"}]}`)}, nil
	default:
		return kleinanzeigen.Response{StatusCode: http.StatusNotFound}, nil
	}
}
