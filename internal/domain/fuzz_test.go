package domain

import (
	"testing"
)

// FuzzDecodeCursor exercises cursor decode/encode round-trips. A decoded cursor
// must always re-encode and decode back to the identical payload.
func FuzzDecodeCursor(f *testing.F) {
	f.Add("cur_eyJ2IjoxLCJwcm9maWxlX3V1aWQiOiIxMjNlNDU2Ny1lODliLTEyZDMtYTQ1Ni00MjY2MTQxNzQwMDAiLCJhY2NvdW50X3N1YmplY3RfaGFzaCI6ImFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWEiLCJnZW5lcmF0aW9uIjowLCJzZXF1ZW5jZSI6MH0")
	f.Add("cur_")
	f.Add("cur_!!")
	f.Add("cur_AA==")
	f.Add("")
	f.Add("evil")
	f.Add("cur_eyJvbmx5Ijp0cnVlfQ")
	f.Add("cur_" + "A")
	f.Fuzz(func(t *testing.T, s string) {
		p, err := DecodeCursor(s)
		if err != nil {
			return
		}
		encoded, err := EncodeCursor(p)
		if err != nil {
			t.Fatalf("re-encode failed for decoded cursor %q: %v", s, err)
		}
		round, err := DecodeCursor(encoded)
		if err != nil {
			t.Fatalf("decode of re-encoded cursor failed: %v", err)
		}
		if round != p {
			t.Fatalf("cursor round-trip mismatch: %+v != %+v", round, p)
		}
	})
}
