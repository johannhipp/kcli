package output

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

func TestNDJSONKeepsMetadataWithProjectedRows(t *testing.T) {
	for _, fields := range [][]string{nil, {"data.id"}, {"warnings"}} {
		value := map[string]any{
			"schema": "kcli.search-results/v1", "request_id": "req_fixture", "observed_at": "2026-09-18T00:00:00Z",
			"source": "public-web", "completeness": "partial", "next": "1",
			"page":     map[string]any{"number": 0, "size": 25, "fetched": 2, "returned": 1},
			"warnings": []any{map[string]any{"code": "page_truncated", "message": "fixture warning"}},
			"data":     []any{map[string]any{"id": "42", "title": "Fixture"}},
		}
		var out bytes.Buffer
		if err := (Encoder{Format: FormatNDJSON, Fields: fields}).Encode(&out, value); err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(&out)
		var rows []map[string]any
		for {
			var row map[string]any
			err := decoder.Decode(&row)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		last := rows[len(rows)-1]
		if last["schema"] != "kcli.summary/v1" {
			t.Fatalf("missing summary for fields %v: %v", fields, rows)
		}
		// Normalize both sides through JSON since the decoder uses float64 numbers.
		data, _ := json.Marshal(value)
		var expected map[string]any
		if err := json.Unmarshal(data, &expected); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"request_id", "observed_at", "source", "completeness", "next", "page", "warnings"} {
			if !reflect.DeepEqual(last[key], expected[key]) {
				t.Errorf("fields=%v lost %s: got=%v want=%v", fields, key, last[key], expected[key])
			}
		}
		if len(fields) > 0 && fields[0] == "data.id" && (len(rows) != 2 || len(rows[0]) != 1 || rows[0]["id"] != "42") {
			t.Fatalf("unprojected rows: %v", rows)
		}
		if len(fields) > 0 && fields[0] == "warnings" && len(rows) != 1 {
			t.Fatalf("unrequested rows: %v", rows)
		}
	}
}
