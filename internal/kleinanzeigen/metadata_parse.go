package kleinanzeigen

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
)

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
	if len(out) == 0 && len(rows) > 0 {
		return nil, warnings, &domain.Error{Code: domain.CodeUpstreamContract, Message: "location response contained no usable location identities"}
	}
	if out == nil {
		out = []domain.LocationV1{}
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
	if searchParam == "web-link" || searchParam == "clickableOptions" || strings.HasPrefix(searchParam, "attributeMap[") {
		return "public-web-advertised", "accepted"
	}
	return "fixture-eq", "accepted"
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

func firstFieldString(object map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := fieldString(object, name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}
