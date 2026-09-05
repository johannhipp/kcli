package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/johannhipp/kcli/internal/app"
)

type Operation struct {
	Path                 string         `json:"path"`
	Purpose              string         `json:"purpose"`
	Input                string         `json:"input"`
	Output               string         `json:"output"`
	SchemaVersion        string         `json:"schema_version"`
	AuthRequired         bool           `json:"auth_required"`
	SideEffect           app.SideEffect `json:"side_effect"`
	ConfirmationRequired bool           `json:"confirmation_required"`
	Evidence             app.Evidence   `json:"evidence"`
	DefaultLimit         int            `json:"default_limit,omitempty"`
	HardLimit            int            `json:"hard_limit,omitempty"`
	Examples             []string       `json:"examples"`
	StoryIDs             []string       `json:"story_ids"`
}
type Document struct {
	Operation Operation          `json:"operation"`
	Input     *jsonschema.Schema `json:"input_schema"`
	Output    *jsonschema.Schema `json:"output_schema"`
}
type FilterOverlay func(context.Context, string, *jsonschema.Schema) (*jsonschema.Schema, error)
type Catalog struct {
	operations    *app.Catalog
	filterOverlay FilterOverlay
}

func New(operations *app.Catalog, overlay FilterOverlay) *Catalog {
	return &Catalog{operations: operations, filterOverlay: overlay}
}

func (c *Catalog) List() []Operation {
	metas := c.operations.Operations()
	result := make([]Operation, len(metas))
	for i, meta := range metas {
		result[i] = describe(meta)
	}
	return result
}
func (c *Catalog) Show(path string) (Document, error) {
	meta, ok := c.operations.Find(path)
	if !ok {
		return Document{}, fmt.Errorf("unknown command %q", path)
	}
	input, err := infer(meta.InputType, meta.InputName)
	if err != nil {
		return Document{}, err
	}
	output, err := infer(meta.OutputType, meta.OutputName)
	if err != nil {
		return Document{}, err
	}
	return Document{Operation: describe(meta), Input: input, Output: output}, nil
}

// ValidateFields checks that each dot-separated --fields path names a real
// output field for the operation, descending object properties and array item
// schemas. It lets callers reject a guessed or stale path before a command runs
// rather than after work has already been done.
func (c *Catalog) ValidateFields(path string, fields []string) error {
	doc, err := c.Show(path)
	if err != nil {
		return err
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return fmt.Errorf("field paths cannot be empty")
		}
		if err := validateFieldPath(doc.Output, strings.Split(field, "."), field); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldPath(schema *jsonschema.Schema, parts []string, full string) error {
	if schema == nil {
		return fmt.Errorf("field path %q is not in the output schema", full)
	}
	if len(parts) == 0 {
		return nil
	}
	if schema.Properties != nil {
		if child, ok := schema.Properties[parts[0]]; ok {
			return validateFieldPath(child, parts[1:], full)
		}
	}
	// The path continues into the elements of an array schema.
	if schema.Items != nil {
		if validateFieldPath(schema.Items, parts, full) == nil {
			return nil
		}
	}
	for _, item := range schema.ItemsArray {
		if item != nil && validateFieldPath(item, parts, full) == nil {
			return nil
		}
	}
	for _, item := range schema.PrefixItems {
		if item != nil && validateFieldPath(item, parts, full) == nil {
			return nil
		}
	}
	if schema.AdditionalProperties != nil {
		return validateFieldPath(schema.AdditionalProperties, parts, full)
	}
	return fmt.Errorf("field path %q is not in the output schema", full)
}

func (c *Catalog) Filters(ctx context.Context, category string) (*jsonschema.Schema, error) {
	meta, ok := c.operations.Find("search")
	if !ok {
		return nil, fmt.Errorf("search schema is unavailable")
	}
	static, err := infer(meta.InputType, meta.InputName)
	if err != nil {
		return nil, err
	}
	if c.filterOverlay == nil {
		static.Comment = "live category filter overlay is unavailable until metadata is cached in Phase 2"
		return static, nil
	}
	return c.filterOverlay(ctx, category, static)
}
func (c *Catalog) Validate(path string, input []byte) error {
	doc, err := c.Show(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	var instance any
	if err := dec.Decode(&instance); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("input contains trailing data")
	}
	resolved, err := doc.Input.Resolve(nil)
	if err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}
	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("validate input: %w", err)
	}
	return nil
}
func infer(value reflect.Type, name string) (*jsonschema.Schema, error) {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	schema, err := jsonschema.ForType(value, nil)
	if err != nil {
		return nil, fmt.Errorf("infer %s: %w", name, err)
	}
	schema.Schema = "https://json-schema.org/draft/2020-12/schema"
	schema.Title = name
	decorate(schema, name)
	return schema, nil
}
func decorate(schema *jsonschema.Schema, name string) {
	enum := func(property string, values ...string) {
		if field := schema.Properties[property]; field != nil {
			field.Enum = make([]any, len(values))
			for i, value := range values {
				field.Enum[i] = value
			}
		}
	}
	bounds := func(property string, minimum, maximum float64) {
		if field := schema.Properties[property]; field != nil {
			field.Minimum = &minimum
			field.Maximum = &maximum
		}
	}
	switch name {
	case "SearchInputV1":
		enum("ad_type", "offered", "wanted")
		enum("sort", "date-desc", "price-asc", "price-desc", "distance-asc")
		bounds("page", 0, 1<<31-1)
		bounds("page_size", 1, 25)
		bounds("limit", 1, 1000)
	case "SellerSearchInputV1":
		enum("match", "exact", "contains")
	case "CompletionInputV1":
		enum("shell", "bash", "fish", "zsh")
	case "DMListInputV1":
		bounds("page", 0, 1<<31-1)
		bounds("page_size", 1, 100)
		bounds("limit", 1, 500)
	case "DMPollInputV1", "DMWatchInputV1":
		bounds("limit", 1, 500)
	}
}
func describe(meta app.OperationMeta) Operation {
	examples := append([]string{}, meta.Examples...)
	stories := append([]string{}, meta.StoryIDs...)
	return Operation{Path: meta.Path, Purpose: meta.Purpose, Input: meta.InputName, Output: meta.OutputName, SchemaVersion: meta.SchemaVersion, AuthRequired: meta.AuthRequired, SideEffect: meta.SideEffect, ConfirmationRequired: meta.ConfirmationRequired, Evidence: meta.Evidence, DefaultLimit: meta.DefaultLimit, HardLimit: meta.HardLimit, Examples: examples, StoryIDs: stories}
}
