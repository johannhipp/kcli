package cli

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
	schemacatalog "github.com/johannhipp/kcli/internal/schema"
)

func TestValidateFieldsAgainstSchema(t *testing.T) {
	root := &Root{}
	parser, err := kong.New(root, kong.Name("kcli"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := app.BuildCatalog(parser.Model)
	if err != nil {
		t.Fatal(err)
	}
	sc := schemacatalog.New(catalog, nil)

	for _, ok := range [][]string{{"data.title", "data.price"}, {"schema"}, {"data.id"}} {
		if err := sc.ValidateFields("search", ok); err != nil {
			t.Errorf("valid fields %v rejected: %v", ok, err)
		}
	}
	for _, bad := range [][]string{{"data.amount"}, {"data.bogus"}, {"bogus.x"}, {""}} {
		if err := sc.ValidateFields("search", bad); err == nil {
			t.Errorf("invalid fields %v accepted", bad)
		}
	}
}
