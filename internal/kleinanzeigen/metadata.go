package kleinanzeigen

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	return MetadataResult[[]domain.CategoryV1]{Data: parsed, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: transportSource(s.transport), ObservedAt: now, Completeness: completeness}, nil
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
			return MetadataResult[[]domain.LocationV1]{}, fmt.Errorf("public location endpoint failed: %w", err)
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
	return MetadataResult[[]domain.LocationV1]{Data: items, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: transportSource(s.transport), ObservedAt: now, Completeness: completeness}, nil
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
	return MetadataResult[[]domain.FilterV1]{Data: items, Raw: append(json.RawMessage(nil), raw...), Warnings: warnings, Source: transportSource(s.transport), ObservedAt: now, Completeness: completeness}, nil
}

// CachedFilters never refreshes metadata or performs a request.
func (s *MetadataService) CachedFilters(ctx context.Context, reference string) (MetadataResult[[]domain.FilterV1], error) {
	if !validMetadataInput(reference, 512) {
		return MetadataResult[[]domain.FilterV1]{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid category reference"}
	}
	categories, err := s.state.ListCategories(ctx)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	if len(categories) == 0 {
		return MetadataResult[[]domain.FilterV1]{}, &domain.Error{Code: domain.CodeNotFound, Message: "category metadata is not cached; run category list"}
	}
	category, err := resolveCategory(categoryCacheResult(categories).Data, reference)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	cached, err := s.state.ListFilters(ctx, category.ID)
	if err != nil {
		return MetadataResult[[]domain.FilterV1]{}, err
	}
	if len(cached) == 0 {
		return MetadataResult[[]domain.FilterV1]{}, &domain.Error{Code: domain.CodeNotFound, Message: "filter metadata is not cached; run filter list --category ID --refresh"}
	}
	return filterCacheResult(cached), nil
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

func validMetadataInput(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
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
