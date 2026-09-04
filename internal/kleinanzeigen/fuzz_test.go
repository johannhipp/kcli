package kleinanzeigen

import (
	"testing"
)

// FuzzDecodeJSON exercises the mobile-API decode chain — JSON decode, wrapper
// value unwrapping, documented-field HTML unescaping, singleton normalization,
// key lookup, and duplicate-key rejection — over arbitrary bytes. The success
// cases mirror real wrapped responses; the failure cases ensure the helpers
// never panic on malformed or hostile input.
func FuzzDecodeJSON(f *testing.F) {
	f.Add([]byte(`{"value":{"location":[{"id":"1","localized-name":"Berlin"}]}}`))
	f.Add([]byte(`{"ad":{"id":"123","title":"Tom &amp; Jerry"}}`))
	f.Add([]byte(`{"ads":{"value":{"paging":{"numFound":"25"}}}}`))
	f.Add([]byte(`[{"id":"1"},{"id":"2"}]`))
	f.Add([]byte(`{"title":"a"} {"title":"b"}`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"a":{"value":{"value":"x"}},"a":1}`))
	f.Add([]byte(`{"price":{"amount":"12,50"}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		decoded, err := DecodeJSON(raw)
		if err != nil {
			return
		}
		value := UnwrapValues(decoded)
		value = HTMLUnescapeDocumentedFields(value)
		_, _ = NormalizeSingleton(value)
		_, _ = FindKey(value, "location")
		_, _ = FindKey(value, "ad")
		_, _ = StringValue(value)
		var dst map[string]any
		_ = DecodeUniqueJSON(raw, &dst)
	})
}

// FuzzListingReferenceID exercises the strict listing reference parser. Any
// accepted reference must resolve to a non-empty digit-only listing id.
func FuzzListingReferenceID(f *testing.F) {
	f.Add("1234567890")
	f.Add("https://www.kleinanzeigen.de/s-anzeige/title/1234567890-1-2")
	f.Add("https://kleinanzeigen.de/s-anzeige/title/123-456-789/")
	f.Add("https://www.kleinanzeigen.de/s-anzeige/title/123-456-789?q=1")
	f.Add("https://evil.example.com/s-anzeige/x/1-2-3")
	f.Add("%2e%2e")
	f.Add("..")
	f.Add("")
	f.Add("abc")
	f.Add("123-456-789")
	f.Add("https://www.kleinanzeigen.de/s-anzeige/x/1-2-3#frag")
	f.Fuzz(func(t *testing.T, ref string) {
		id, err := ListingReferenceID(ref)
		if err != nil {
			return
		}
		if id == "" {
			t.Fatalf("ListingReferenceID returned an empty id for %q", ref)
		}
		for _, r := range id {
			if r < '0' || r > '9' {
				t.Fatalf("ListingReferenceID returned non-digit id %q for %q", id, ref)
			}
		}
	})
}

// FuzzRedactText ensures the redaction helpers never panic and always return a
// string on arbitrary input.
func FuzzRedactText(f *testing.F) {
	f.Add("Bearer abc.def.ghi")
	f.Add("Authorization: Bearer xyz")
	f.Add("Basic dXNlcjpwYXNz")
	f.Add("contact user@example.com")
	f.Add("user%40example%2Ecom")
	f.Add("plain sentence, no secrets")
	f.Add("")
	f.Add("\x00\x01\r\n")
	f.Fuzz(func(t *testing.T, s string) {
		_ = RedactText(s)
		_ = RedactURL(s)
	})
}
