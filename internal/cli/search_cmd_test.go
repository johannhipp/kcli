package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestSearchCommandInputPreservesRepeatedFilters(t *testing.T) {
	pageSize := 10
	limit := 30
	adType := "wanted"
	command := &SearchCmd{Query: "bike", PageSize: &pageSize, Limit: &limit, AdType: &adType, Paginate: true, Filter: []string{"color=red", "color=blue"}}
	input, err := searchCommandInput(&Runtime{}, command)
	if err != nil {
		t.Fatal(err)
	}
	if input.Query != "bike" || input.PageSize != 10 || input.Limit != 30 || input.AdType != "wanted" || len(input.Filters) != 2 {
		t.Fatalf("input = %#v", input)
	}
}

func TestSearchCommandInputReadsVersionedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.json")
	if err := os.WriteFile(path, []byte(`{"schema":"kcli.search-input/v1","query":"bike","page_size":5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := searchCommandInput(&Runtime{}, &SearchCmd{Input: path})
	if err != nil {
		t.Fatal(err)
	}
	if input.Query != "bike" || input.PageSize != 5 {
		t.Fatalf("input = %#v", input)
	}
}

func TestSearchCommandInputRejectsWrongSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.json")
	if err := os.WriteFile(path, []byte(`{"schema":"kcli.search-input/v2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := searchCommandInput(&Runtime{}, &SearchCmd{Input: path})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidSchema || domain.ExitCode(err) != 2 {
		t.Fatalf("error = %#v", err)
	}
}

func TestSearchCommandInputRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, body := range []string{
		`{"schema":"kcli.search-input/v1","future":true}`,
		`{"schema":"kcli.search-input/v1","query":"one","query":"two"}`,
	} {
		path := filepath.Join(t.TempDir(), "search.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := searchCommandInput(&Runtime{}, &SearchCmd{Input: path})
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidSchema {
			t.Fatalf("body %s: error = %#v", body, err)
		}
	}
}
