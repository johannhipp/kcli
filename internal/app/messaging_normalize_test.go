package app

import "testing"

func TestMessageReconciliationNormalizationNFCWhitespace(t *testing.T) {
	composed := "  Caf\u00e9\t[REDACTED]\nvalue  "
	decomposed := "Cafe\u0301 [REDACTED]   value"
	if normalizeMessageForReconciliation(composed) != normalizeMessageForReconciliation(decomposed) {
		t.Fatalf("normalized values differ: %q != %q", normalizeMessageForReconciliation(composed), normalizeMessageForReconciliation(decomposed))
	}
}
