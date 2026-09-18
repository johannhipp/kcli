package schema

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/johannhipp/kcli/internal/domain"
	"strings"
)

// OverlayFilters describes advertised keys and values. Search still validates
// serialization and cross-field constraints. Unknown definitions stay visible.
func OverlayFilters(schema *jsonschema.Schema, definitions []domain.FilterV1) *jsonschema.Schema {
	var choices []*jsonschema.Schema
	for _, definition := range definitions {
		if definition.Classification == "unsupported-client" || definition.Classification == "unsupported-upstream" {
			continue
		}
		value := &jsonschema.Schema{Type: "string"}
		for _, option := range definition.SupportedValues {
			if strings.EqualFold(definition.Type, "enum") {
				value.Enum = append(value.Enum, option.Value)
			} else {
				value.Examples = append(value.Examples, option.Value)
			}
		}
		choices = append(choices, &jsonschema.Schema{
			Type: "object", Required: []string{"key", "value"}, Description: definition.Label,
			Properties: map[string]*jsonschema.Schema{
				"key": {Type: "string", Enum: []any{definition.Key}}, "value": value,
			},
			AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		})
	}
	items := &jsonschema.Schema{OneOf: choices}
	if len(choices) == 0 {
		items = &jsonschema.Schema{Not: &jsonschema.Schema{}}
	}
	schema.Properties["filters"].Items = items
	schema.Extra = map[string]any{"x-kcli-filter-metadata": definitions}
	schema.Comment = "Cached metadata; refresh with filter list --refresh. Search validates serialization and cross-field constraints."
	return schema
}
