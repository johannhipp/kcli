package app

import (
	"context"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

var searchDecimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,2})?$`)
var searchIntegerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func searchCanonicalInput(input domain.SearchInputV1) (domain.SearchInputV1, error) {
	if input.Schema != "" && input.Schema != "kcli.search-input/v1" {
		return input, &domain.Error{Code: domain.CodeInvalidSchema, Message: "search input schema must be kcli.search-input/v1"}
	}
	input.Schema = "kcli.search-input/v1"
	if input.PageSize == 0 {
		input.PageSize = 25
	}
	if input.AdType == "" {
		input.AdType = "offered"
	}
	if input.Sort == "" {
		input.Sort = "date-desc"
	}
	if input.Limit == 0 {
		if input.Paginate {
			input.Limit = 100
		} else {
			input.Limit = input.PageSize
		}
	}
	if input.Page < 0 {
		return input, searchInvalid("page must not be negative", nil)
	}
	if input.PageSize < 1 || input.PageSize > 25 {
		return input, searchInvalid("page_size must be between 1 and 25", nil)
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return input, searchInvalid("limit must be between 1 and 1000", nil)
	}
	if input.Radius < 0 {
		return input, searchInvalid("radius must not be negative", nil)
	}
	if !searchValidOptionalText(input.Query, 4096) || !searchValidOptionalText(input.Category, 512) || !searchValidOptionalText(input.Location, 256) {
		return input, searchInvalid("query, category, or location contains invalid text", nil)
	}
	if !searchValidPrice(input.MinPrice) || !searchValidPrice(input.MaxPrice) {
		return input, searchInvalid("prices must be non-negative decimal euro strings with at most two fractional digits", nil)
	}
	if input.MinPrice != "" && input.MaxPrice != "" && searchCompareDecimal(input.MinPrice, input.MaxPrice) > 0 {
		return input, searchInvalid("min_price must not exceed max_price", nil)
	}
	if input.AdType != "offered" && input.AdType != "wanted" {
		return input, searchInvalid("ad_type must be offered or wanted", nil)
	}
	switch input.Sort {
	case "date-desc", "price-asc", "price-desc", "distance-asc":
	default:
		return input, searchInvalid("sort is not supported", nil)
	}
	for _, exclusion := range input.Exclusions {
		if strings.TrimSpace(exclusion) == "" || !searchValidRequiredText(exclusion, 4096) {
			return input, searchInvalid("exclusions must be non-empty text without control characters", nil)
		}
	}
	for _, filter := range input.Filters {
		if !searchValidRequiredText(filter.Key, 128) || !searchValidRequiredText(filter.Value, 4096) {
			return input, searchInvalid("dynamic filters require non-empty keys and values without control characters", nil)
		}
		if searchCommonFilterKey(filter.Key) {
			return input, searchInvalid("common search flags cannot be supplied as dynamic filters", map[string]any{"key": filter.Key})
		}
	}
	return input, nil
}

func searchResolveLocation(ctx context.Context, metadata *kleinanzeigen.MetadataService, reference string) (kleinanzeigen.MetadataResult[domain.LocationV1], error) {
	// Numeric references are IDs; postcode text is resolved explicitly through
	// location resolve so it cannot be confused with an upstream location ID.
	if searchNumeric(reference) {
		return kleinanzeigen.MetadataResult[domain.LocationV1]{Data: domain.LocationV1{ID: reference}, Warnings: []domain.WarningV1{}, Source: "input", Completeness: domain.CompletenessBestEffort}, nil
	}
	result, err := metadata.Locations(ctx, reference, 100)
	if err != nil {
		return kleinanzeigen.MetadataResult[domain.LocationV1]{}, err
	}
	folded := searchFold(reference)
	matches := make([]domain.LocationV1, 0, len(result.Data))
	for _, location := range result.Data {
		if location.ID == reference || searchFold(location.Label) == folded {
			matches = append(matches, location)
		}
	}
	if len(matches) == 0 && len(result.Data) == 1 && !searchNumeric(reference) {
		matches = append(matches, result.Data[0])
	}
	if len(matches) != 1 {
		candidates := make([]map[string]string, 0, len(result.Data))
		for _, location := range result.Data {
			candidates = append(candidates, map[string]string{"id": location.ID, "label": location.Label})
		}
		return kleinanzeigen.MetadataResult[domain.LocationV1]{}, searchInvalid("location reference did not resolve uniquely", map[string]any{"reference": reference, "candidates": candidates})
	}
	return kleinanzeigen.MetadataResult[domain.LocationV1]{Data: matches[0], Raw: matches[0].Raw, Warnings: result.Warnings, Source: result.Source, ObservedAt: result.ObservedAt, Completeness: result.Completeness}, nil
}

func searchBuildQuery(ctx context.Context, metadata *kleinanzeigen.MetadataService, input *domain.SearchInputV1) (map[string][]string, []domain.WarningV1, error) {
	query := map[string][]string{
		"size":     {strconv.Itoa(input.PageSize)},
		"adType":   {map[string]string{"offered": "OFFERED", "wanted": "WANTED"}[input.AdType]},
		"sortType": {map[string]string{"date-desc": "DATE_DESCENDING", "price-asc": "PRICE_ASCENDING", "price-desc": "PRICE_DESCENDING", "distance-asc": "DISTANCE_ASCENDING"}[input.Sort]},
	}
	searchSetQuery(query, "q", input.Query)
	searchSetQuery(query, "categoryId", input.Category)
	searchSetQuery(query, "locationId", input.Location)
	if input.Radius > 0 {
		query["distance"] = []string{strconv.Itoa(input.Radius)}
	}
	searchSetQuery(query, "minPrice", input.MinPrice)
	searchSetQuery(query, "maxPrice", input.MaxPrice)
	if input.PictureRequired {
		query["pictureRequired"] = []string{"true"}
	}
	if len(input.Filters) == 0 {
		return query, nil, nil
	}
	if input.Category == "" {
		return nil, nil, searchInvalid("dynamic filters require a category", nil)
	}
	definitions, err := metadata.Filters(ctx, input.Category, false)
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[string]domain.FilterV1, len(definitions.Data))
	for _, definition := range definitions.Data {
		byKey[strings.ToLower(definition.Key)] = definition
	}
	usedEq := make(map[string]bool)
	for index, filter := range input.Filters {
		definition, ok := byKey[strings.ToLower(filter.Key)]
		if !ok {
			return nil, nil, searchInvalid("dynamic filter is not present in current category metadata", map[string]any{"key": filter.Key, "inspect": "kcli filter get --category " + input.Category + " " + filter.Key + " --output raw"})
		}
		if definition.Classification == "unsupported-upstream" || definition.Classification == "unsupported-client" {
			return nil, nil, searchInvalid("dynamic filter is not safely serializable", map[string]any{"key": definition.Key, "classification": definition.Classification, "inspect": "kcli filter get --category " + input.Category + " " + definition.Key + " --output raw"})
		}
		style := strings.ToLower(definition.SearchStyle)
		if style != "eq" && style != "in" {
			return nil, nil, searchInvalid("dynamic filter search style is unproven", map[string]any{"key": definition.Key, "search_style": definition.SearchStyle, "inspect": "kcli filter get --category " + input.Category + " " + definition.Key + " --output raw"})
		}
		if err := searchValidateFilterValue(definition, filter.Value); err != nil {
			return nil, nil, err
		}
		input.Filters[index].Key = definition.Key
		if style == "eq" {
			if usedEq[definition.Key] {
				return nil, nil, searchInvalid("eq-style dynamic filter accepts one value", map[string]any{"key": definition.Key})
			}
			usedEq[definition.Key] = true
			query[definition.Key] = []string{filter.Value}
		} else {
			query[definition.Key] = append(query[definition.Key], filter.Value)
		}
	}
	return query, definitions.Warnings, nil
}

func searchValidateFilterValue(definition domain.FilterV1, value string) error {
	switch strings.ToLower(definition.Type) {
	case "string", "text":
	case "integer", "int", "long":
		if !searchIntegerPattern.MatchString(value) {
			return searchInvalid("dynamic filter requires an integer", map[string]any{"key": definition.Key})
		}
	case "decimal", "double", "number":
		if !searchDecimalPattern.MatchString(value) {
			return searchInvalid("dynamic filter requires an exact decimal", map[string]any{"key": definition.Key})
		}
	case "boolean", "bool":
		if value != "true" && value != "false" {
			return searchInvalid("dynamic filter requires true or false", map[string]any{"key": definition.Key})
		}
	case "enum":
		valid := false
		for _, supported := range definition.SupportedValues {
			if value == supported.Value {
				valid = true
				break
			}
		}
		if !valid {
			return searchInvalid("dynamic filter value is not supported", map[string]any{"key": definition.Key, "value": value})
		}
	case "date":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return searchInvalid("dynamic filter requires an ISO date", map[string]any{"key": definition.Key})
		}
	case "datetime", "date-time":
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return searchInvalid("dynamic filter requires an RFC 3339 timestamp", map[string]any{"key": definition.Key})
		}
	default:
		return searchInvalid("dynamic filter type is unproven", map[string]any{"key": definition.Key, "type": definition.Type})
	}
	return nil
}

func searchValidPrice(value string) bool {
	return value == "" || searchDecimalPattern.MatchString(value)
}

func searchCompareDecimal(left, right string) int {
	leftValue, _ := new(big.Rat).SetString(left)
	rightValue, _ := new(big.Rat).SetString(right)
	return leftValue.Cmp(rightValue)
}

func searchCommonFilterKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(key))
	common := map[string]bool{"page": true, "size": true, "pagesize": true, "category": true, "categoryid": true, "location": true, "locationid": true, "radius": true, "distance": true, "minprice": true, "maxprice": true, "adtype": true, "q": true, "query": true, "picturerequired": true, "sort": true, "sorttype": true, "paginate": true, "limit": true, "exclude": true, "exclusions": true}
	return common[normalized]
}

func searchValidOptionalText(value string, max int) bool {
	return value == "" || searchValidRequiredText(value, max)
}

func searchValidRequiredText(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func searchFold(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func searchNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func searchSetQuery(query map[string][]string, key, value string) {
	if value != "" {
		query[key] = []string{value}
	}
}

func searchInvalid(message string, details map[string]any) *domain.Error {
	return &domain.Error{Code: domain.CodeInvalidInput, Message: message, Details: details}
}
