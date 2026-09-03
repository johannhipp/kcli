package domain

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCursorRoundTrip(t *testing.T) {
	want := CursorPayloadV1{FormatVersion: 1, ProfileUUID: "123e4567-e89b-42d3-a456-426614174000", AccountSubjectHash: strings.Repeat("a", 64), Generation: 2, Sequence: 41}
	encoded, err := EncodeCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("cursor mismatch (-want +got):\n%s", diff)
	}
}
func TestCursorRejectsWrongPrefix(t *testing.T) {
	if _, err := DecodeCursor("bad_value"); err == nil {
		t.Fatal("expected invalid cursor")
	}
}
