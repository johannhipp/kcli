package output

import (
	"errors"
	"testing"
)

type failingTableWriter struct{ err error }

func (w failingTableWriter) Write([]byte) (int, error) { return 0, w.err }

func TestTableReportsWriteFailure(t *testing.T) {
	failure := errors.New("fixture write failure")
	for _, value := range []any{map[string]any{"id": "1"}, []any{map[string]any{"id": "1"}}, []any{}, "scalar"} {
		err := (Encoder{Format: FormatTable}).Encode(failingTableWriter{failure}, value)
		if !errors.Is(err, failure) {
			t.Fatalf("%T: got %v, want write failure", value, err)
		}
	}
}
