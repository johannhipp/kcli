package kleinanzeigen

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/state"
)

const categoryTTL = 24 * time.Hour
const locationTTL = 7 * 24 * time.Hour

type metadataClock interface{ Now() time.Time }
type systemMetadataClock struct{}

func (systemMetadataClock) Now() time.Time { return time.Now().UTC() }

type MetadataResult[T any] struct {
	Data         T
	Raw          json.RawMessage
	Warnings     []domain.WarningV1
	Source       string
	ObservedAt   time.Time
	Completeness domain.Completeness
}

type MetadataService struct {
	transport Transport
	state     *state.DB
	clock     metadataClock
}

func NewMetadataService(transport Transport, database *state.DB) *MetadataService {
	return &MetadataService{transport: transport, state: database, clock: systemMetadataClock{}}
}

func (s *MetadataService) Categories(ctx context.Context, refresh bool) (MetadataResult[[]domain.CategoryV1], error) {
	cached, err := s.state.ListCategories(ctx)
	if err != nil {
		return MetadataResult[[]domain.CategoryV1]{}, fmt.Errorf("read category cache: %w", err)
	}
	fresh := len(cached) > 0 && s.clock.Now().Sub(cached[0].ObservedAt) < categoryTTL
	if !refresh && fresh {
		return categoryCacheResult(cached), nil
	}
	raw, err := s.fetch(ctx, "/api/categories.json", nil)
	if err != nil {
		return MetadataResult[[]domain.CategoryV1]{}, err
	}
	parsed, warnings, err := ParseCategories(raw)
	if err != nil {
		return MetadataResult[[]domain.CategoryV1]{}, err
	}
	now := s.clock.Now().UTC()
	records := make([]state.CategorySnapshot, 0, len(parsed))
	for _, item := range parsed {
		records = append(records, state.CategorySnapshot{ID: item.ID, Path: item.Path, Label: item.Label, ParentID: item.ParentID, RawJSON: item.Raw, ObservedAt: now})
	}
	if err := s.state.ReplaceCategories(ctx, records); err != nil {
		return MetadataResult[[]domain.CategoryV1]{}, fmt.Errorf("write category cache: %w", err)
	}
	completeness := domain.CompletenessComplete
	if len(warnings) > 0 {
		completeness = domain.CompletenessPartial
	}
	return MetadataResult[[]domain.CategoryV1]{Data: parsed, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: "mobile-api", ObservedAt: now, Completeness: completeness}, nil
}

func categoryCacheResult(cached []state.CategorySnapshot) MetadataResult[[]domain.CategoryV1] {
	items := make([]domain.CategoryV1, 0, len(cached))
	rawItems := make([]json.RawMessage, 0, len(cached))
	for _, item := range cached {
		items = append(items, domain.CategoryV1{ID: item.ID, Path: item.Path, Label: item.Label, ParentID: item.ParentID, Raw: item.RawJSON})
		rawItems = append(rawItems, item.RawJSON)
	}
	raw, _ := json.Marshal(rawItems)
	return MetadataResult[[]domain.CategoryV1]{Data: items, Raw: raw, Warnings: []domain.WarningV1{}, Source: "cache", ObservedAt: cached[0].ObservedAt, Completeness: domain.CompletenessComplete}
}

func (s *MetadataService) Category(ctx context.Context, reference string) (MetadataResult[domain.CategoryV1], error) {
	if !validMetadataInput(reference, 512) {
		return MetadataResult[domain.CategoryV1]{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid category reference"}
	}
	all, err := s.Categories(ctx, false)
	if err != nil {
		return MetadataResult[domain.CategoryV1]{}, err
	}
	match, err := resolveCategory(all.Data, reference)
	if err != nil {
		return MetadataResult[domain.CategoryV1]{}, err
	}
	return MetadataResult[domain.CategoryV1]{Data: match, Raw: match.Raw, Warnings: all.Warnings, Source: all.Source, ObservedAt: all.ObservedAt, Completeness: all.Completeness}, nil
}

func (s *MetadataService) SearchCategories(ctx context.Context, text string) (MetadataResult[[]domain.CategoryV1], error) {
	if !validMetadataInput(text, 256) {
		return MetadataResult[[]domain.CategoryV1]{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid category search text"}
	}
	all, err := s.Categories(ctx, false)
	if err != nil {
		return MetadataResult[[]domain.CategoryV1]{}, err
	}
	needle := fold(text)
	matches := make([]domain.CategoryV1, 0)
	for _, item := range all.Data {
		if strings.Contains(fold(item.Label), needle) || strings.Contains(fold(item.Path), needle) {
			matches = append(matches, item)
		}
	}
	all.Data = matches
	raw, _ := json.Marshal(matches)
	all.Raw = raw
	return all, nil
}
func (s *MetadataService) Locations(ctx context.Context, text string, limit int) (MetadataResult[[]domain.LocationV1], error) {
	if !validMetadataInput(text, 256) || limit < 1 || limit > 100 {
		return MetadataResult[[]domain.LocationV1]{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid location query or limit"}
	}
	queryKey := normalizeQuery(text)
	entry, err := s.state.GetLocationCache(ctx, queryKey)
	if err == nil && entry.ExpiresAt.After(s.clock.Now()) {
		var items []domain.LocationV1
		if json.Unmarshal(entry.ResultJSON, &items) == nil {
			if len(items) > limit {
				items = items[:limit]
			}
			return MetadataResult[[]domain.LocationV1]{Data: items, Raw: entry.ResultJSON, Warnings: []domain.WarningV1{}, Source: "cache", ObservedAt: entry.ObservedAt, Completeness: domain.CompletenessComplete}, nil
		}
	} else if err != nil && err != sql.ErrNoRows {
		return MetadataResult[[]domain.LocationV1]{}, fmt.Errorf("read location cache: %w", err)
	}
	raw, err := s.fetch(ctx, "/api/locations.json", map[string][]string{"q": {text}})
	if err != nil {
		var typed *domain.Error
		if !domainErrorAs(err, &typed) {
			return MetadataResult[[]domain.LocationV1]{}, fmt.Errorf("mobile location endpoint failed: %w", err)
		}
		return MetadataResult[[]domain.LocationV1]{}, err
	}
	items, warnings, err := ParseLocations(raw)
	if err != nil {
		return MetadataResult[[]domain.LocationV1]{}, err
	}
	now := s.clock.Now().UTC()
	cacheJSON, _ := json.Marshal(items)
	if err := s.state.PutLocationCache(ctx, state.LocationCacheEntry{QueryKey: queryKey, ResultJSON: cacheJSON, ObservedAt: now, ExpiresAt: now.Add(locationTTL)}); err != nil {
		return MetadataResult[[]domain.LocationV1]{}, fmt.Errorf("write location cache: %w", err)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	completeness := domain.CompletenessComplete
	if len(warnings) > 0 {
		completeness = domain.CompletenessPartial
	}
	return MetadataResult[[]domain.LocationV1]{Data: items, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: "mobile-api", ObservedAt: now, Completeness: completeness}, nil
}

func (s *MetadataService) Filters(ctx context.Context, categoryReference string, refresh bool) (MetadataResult[[]domain.FilterV1], error) {
	categoryResult, err := s.Category(ctx, categoryReference)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	category := categoryResult.Data
	cached, err := s.state.ListFilters(ctx, category.ID)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, fmt.Errorf("read filter cache: %w", err)
	}
	fresh := len(cached) > 0 && s.clock.Now().Sub(cached[0].ObservedAt) < categoryTTL
	if !refresh && fresh {
		return filterCacheResult(cached), nil
	}
	path := "/api/ads/search-metadata/" + category.ID + ".json"
	raw, err := s.fetch(ctx, path, nil)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	items, warnings, err := ParseFilters(category.ID, raw)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	now := s.clock.Now().UTC()
	records := make([]state.FilterSnapshot, 0, len(items))
	for _, item := range items {
		records = append(records, state.FilterSnapshot{CategoryID: category.ID, Key: item.Key, Type: item.Type, SearchParam: item.SearchParam, SearchStyle: item.SearchStyle, RawJSON: item.Raw, ObservedAt: now, Proof: item.Proof})
	}
	if err := s.state.ReplaceFilters(ctx, category.ID, records); err != nil {
		return MetadataResult[[]domain.FilterV1]{}, fmt.Errorf("write filter cache: %w", err)
	}
	completeness := domain.CompletenessComplete
	if len(warnings) > 0 {
		completeness = domain.CompletenessPartial
	}
	return MetadataResult[[]domain.FilterV1]{Data: items, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: "mobile-api", ObservedAt: now, Completeness: completeness}, nil
}

func filterCacheResult(cached []state.FilterSnapshot) MetadataResult[[]domain.FilterV1] {
	items := make([]domain.FilterV1, 0, len(cached))
	for _, item := range cached {
		filter := filterFromRaw(item.CategoryID, item.Key, item.Type, item.SearchParam, item.SearchStyle, item.Proof, item.RawJSON)
		items = append(items, filter)
	}
	raw, _ := json.Marshal(items)
	return MetadataResult[[]domain.FilterV1]{Data: items, Raw: raw, Warnings: []domain.WarningV1{}, Source: "cache", ObservedAt: cached[0].ObservedAt, Completeness: domain.CompletenessComplete}
}

func (s *MetadataService) Filter(ctx context.Context, categoryReference, key string) (MetadataResult[domain.FilterV1], error) {
	if !validMetadataInput(key, 128) {
		return MetadataResult[domain.FilterV1]{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid filter key"}
	}
	all, err := s.Filters(ctx, categoryReference, false)
	if err != nil {
		return MetadataResult[domain.FilterV1]{}, err
	}
	for _, item := range all.Data {
		if strings.EqualFold(item.Key, key) {
			return MetadataResult[domain.FilterV1]{Data: item, Raw: item.Raw, Warnings: all.Warnings, Source: all.Source, ObservedAt: all.ObservedAt, Completeness: all.Completeness}, nil
		}
	}
	return MetadataResult[domain.FilterV1]{}, &domain.Error{Code: domain.CodeNotFound, Message: "filter was not found", Details: map[string]any{"key": key}}
}

func (s *MetadataService) fetch(ctx context.Context, path string, query map[string][]string) ([]byte, error) {
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	response, err := s.transport.Do(getJSONRequest(requestContext, path, query, StableRead, true))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ResponseError(response)
	}
	return response.Body, nil
}

func ParseCategories(raw []byte) ([]domain.CategoryV1, []domain.WarningV1, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "category response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	collection, ok := FindKey(decoded, "category")
	if !ok {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "category response did not contain categories"}
	}
	rows, err := NormalizeSingleton(collection)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "category collection had an unexpected shape", Cause: err}
	}
	var out []domain.CategoryV1
	var warnings []domain.WarningV1
	seen := make(map[string]bool)
	var walk func([]any, string, string)
	walk = func(items []any, parentID, parentPath string) {
		for _, row := range items {
			object, ok := row.(map[string]any)
			if !ok {
				warnings = append(warnings, ContractWarning("category", row))
				continue
			}
			id, _ := fieldString(object, "id")
			label, _ := firstFieldString(object, "localized-name", "label", "name", "id-name")
			if !numericID(id) || label == "" {
				warnings = append(warnings, ContractWarning("category.identity", row))
				continue
			}
			path := label
			if parentPath != "" {
				path = parentPath + "/" + label
			}
			itemRaw, _ := json.Marshal(object)
			if !seen[id] {
				out = append(out, domain.CategoryV1{ID: id, Path: path, Label: label, ParentID: parentID, Raw: itemRaw})
				seen[id] = true
			}
			if child, exists := directField(object, "category"); exists {
				children, childErr := NormalizeSingleton(child)
				if childErr != nil {
					warnings = append(warnings, ContractWarning("category.category", child))
					continue
				}
				walk(children, id, path)
			}
		}
	}
	walk(rows, "", "")
	if len(out) == 0 {
		return nil, warnings, &domain.Error{Code: domain.CodeUpstreamContract, Message: "category response contained no usable category identities"}
	}
	return out, warnings, nil
}

func ParseLocations(raw []byte) ([]domain.LocationV1, []domain.WarningV1, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "location response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	collection, ok := FindKey(decoded, "location")
	if !ok {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "location response did not contain locations"}
	}
	rows, err := NormalizeSingleton(collection)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "location collection had an unexpected shape", Cause: err}
	}
	var out []domain.LocationV1
	var warnings []domain.WarningV1
	seen := make(map[string]bool)
	var walk func([]any)
	walk = func(items []any) {
		for _, row := range items {
			object, ok := row.(map[string]any)
			if !ok {
				warnings = append(warnings, ContractWarning("location", row))
				continue
			}
			id, _ := fieldString(object, "id")
			label, _ := firstFieldString(object, "localized-name", "id-name", "name")
			if !numericID(id) || label == "" {
				warnings = append(warnings, ContractWarning("location.identity", row))
				continue
			}
			itemRaw, _ := json.Marshal(object)
			if !seen[id] {
				out = append(out, domain.LocationV1{ID: id, Label: label, Raw: itemRaw})
				seen[id] = true
			}
			if child, exists := directField(object, "location"); exists {
				children, childErr := NormalizeSingleton(child)
				if childErr != nil {
					warnings = append(warnings, ContractWarning("location.location", child))
					continue
				}
				walk(children)
			}
		}
	}
	walk(rows)
	if len(out) == 0 {
		return nil, warnings, &domain.Error{Code: domain.CodeUpstreamContract, Message: "location response contained no usable location identities"}
	}
	return out, warnings, nil
}

func ParseFilters(categoryID string, raw []byte) ([]domain.FilterV1, []domain.WarningV1, error) {
	if !numericID(categoryID) {
		return nil, nil, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "category ID must contain decimal digits only"}
	}
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "filter response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	optionsValue, ok := FindKey(decoded, "ads-search-options")
	if !ok {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "filter response did not contain ads-search-options"}
	}
	options, ok := optionsValue.(map[string]any)
	if !ok {
		return nil, nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "filter options had an unexpected shape"}
	}
	items := make([]domain.FilterV1, 0, len(options))
	var warnings []domain.WarningV1
	for key, value := range options {
		if len(key) > 128 || validateQueryKey(key) != nil {
			warnings = append(warnings, ContractWarning("ads-search-options.key", key))
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			warnings = append(warnings, ContractWarning("ads-search-options."+key, value))
			continue
		}
		typeName, _ := fieldString(object, "type")
		searchParam, _ := fieldString(object, "search-param")
		searchStyle, _ := fieldString(object, "search-style")
		proof, classification := classifyFilter(typeName, searchParam, searchStyle)
		itemRaw, _ := json.Marshal(object)
		item := filterFromRaw(categoryID, key, typeName, searchParam, searchStyle, proof, itemRaw)
		item.Classification = classification
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		return strings.ToLower(items[left].Key) < strings.ToLower(items[right].Key)
	})
	return items, warnings, nil
}

func filterFromRaw(categoryID, key, typeName, searchParam, searchStyle, proof string, raw json.RawMessage) domain.FilterV1 {
	item := domain.FilterV1{CategoryID: categoryID, Key: key, Type: typeName, SearchParam: searchParam, SearchStyle: searchStyle, Proof: proof, Raw: append(json.RawMessage(nil), raw...)}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		item.Label, _ = firstFieldString(object, "localized-label", "label")
		if value, ok := directField(object, "supported-value"); ok {
			rows, _ := NormalizeSingleton(value)
			for _, row := range rows {
				candidate, ok := row.(map[string]any)
				if !ok {
					continue
				}
				stored, _ := fieldString(candidate, "value")
				label, _ := firstFieldString(candidate, "localized-label", "label")
				item.SupportedValues = append(item.SupportedValues, domain.FilterSupportedValueV1{Value: stored, Label: label})
			}
		}
	}
	_, item.Classification = classifyFilter(typeName, searchParam, searchStyle)
	return item
}

func classifyFilter(typeName, searchParam, searchStyle string) (string, string) {
	if strings.EqualFold(searchParam, "unsupported") {
		return "upstream-marker", "unsupported-upstream"
	}
	typeKnown := map[string]bool{"string": true, "text": true, "integer": true, "int": true, "long": true, "decimal": true, "double": true, "number": true, "boolean": true, "bool": true, "enum": true, "date": true, "datetime": true, "date-time": true}[strings.ToLower(typeName)]
	style := strings.ToLower(searchStyle)
	if !typeKnown || (style != "eq" && style != "in") {
		return "none", "unsupported-client"
	}
	if style == "in" {
		return "advertised-unproven", "provisional"
	}
	return "fixture-eq", "accepted"
}

func resolveCategory(items []domain.CategoryV1, reference string) (domain.CategoryV1, error) {
	for _, item := range items {
		if item.ID == reference {
			return item, nil
		}
	}
	needle := fold(reference)
	var exact []domain.CategoryV1
	for _, item := range items {
		if fold(item.Path) == needle {
			exact = append(exact, item)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	var candidates []domain.CategoryV1
	for _, item := range items {
		if strings.Contains(fold(item.Path), needle) {
			candidates = append(candidates, item)
			if len(candidates) == 10 {
				break
			}
		}
	}
	if len(candidates) > 0 {
		details := make([]map[string]string, 0, len(candidates))
		for _, item := range candidates {
			details = append(details, map[string]string{"id": item.ID, "path": item.Path})
		}
		return domain.CategoryV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "category reference is ambiguous", Details: map[string]any{"candidates": details}}
	}
	return domain.CategoryV1{}, &domain.Error{Code: domain.CodeNotFound, Message: "category was not found", Details: map[string]any{"reference": reference}}
}

func directField(object map[string]any, name string) (any, bool) {
	for key, value := range object {
		if key == name || strings.HasSuffix(key, "}"+name) || strings.HasSuffix(key, "/"+name) {
			return value, true
		}
	}
	return nil, false
}
func fieldString(object map[string]any, name string) (string, bool) {
	value, ok := directField(object, name)
	if !ok {
		return "", false
	}
	return StringValue(value)
}
func validMetadataInput(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}
func firstFieldString(object map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := fieldString(object, name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}
func numericID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func normalizeQuery(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func fold(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}
func domainErrorAs(err error, target **domain.Error) bool {
	for err != nil {
		if typed, ok := err.(*domain.Error); ok {
			*target = typed
			return true
		}
		interfaceValue, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = interfaceValue.Unwrap()
	}
	return false
}
