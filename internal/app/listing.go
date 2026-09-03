package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/media"
	"github.com/johannhipp/kcli/internal/state"
)

func (a *App) ListingGet(ctx context.Context, input domain.ListingGetInputV1, requestID string) (domain.ListingOutputV1, error) {
	detail, observedAt, err := a.listingDetail(ctx, input.IDOrURL)
	if err != nil {
		return domain.ListingOutputV1{}, err
	}
	envelope := Envelope(a.Clock, "kcli.listing/v1", requestID, "mobile-api", detail.Listing)
	envelope.ObservedAt = observedAt
	envelope.Warnings = detail.Warnings
	if len(detail.Warnings) > 0 {
		envelope.Completeness = domain.CompletenessPartial
	}
	if input.Raw {
		envelope.Raw = append(json.RawMessage(nil), detail.Raw...)
	}
	return domain.ListingOutputV1{Envelope: envelope}, nil
}

func (a *App) ListingImages(ctx context.Context, input domain.ListingImagesInputV1, requestID string) (domain.ListingImagesOutputV1, error) {
	detail, observedAt, err := a.listingDetail(ctx, input.IDOrURL)
	if err != nil {
		return domain.ListingImagesOutputV1{}, err
	}
	items := listingImageOutput(detail.Media)
	if input.Download != "" {
		selected, err := listingSelectMedia(detail.Media, input.Download)
		if err != nil {
			return domain.ListingImagesOutputV1{}, err
		}
		paths := make(map[string]string, len(selected))
		for _, item := range selected {
			path, err := media.Download(ctx, a.Transport, media.DownloadRequest{ListingID: detail.Listing.ID, Index: item.Index, Relation: item.Relation, URL: item.URL, OutputDir: input.OutputDir, AllowOutsideCWD: input.AllowOutsideCWD, MaxBytes: input.MaxBytes, Overwrite: input.Overwrite})
			if err != nil {
				return domain.ListingImagesOutputV1{}, err
			}
			paths[listingMediaKey(item)] = path
		}
		for _, item := range items {
			index, _ := item["index"].(int)
			relation, _ := item["relation"].(string)
			if path, ok := paths[fmt.Sprintf("%d\x00%s", index, relation)]; ok {
				item["downloaded_path"] = path
			}
		}
	}
	envelope := Envelope(a.Clock, "kcli.listing-images/v1", requestID, "mobile-api", items)
	envelope.ObservedAt = observedAt
	envelope.Warnings = detail.Warnings
	if len(detail.Warnings) > 0 {
		envelope.Completeness = domain.CompletenessPartial
	}
	return domain.ListingImagesOutputV1{Envelope: envelope}, nil
}

func (a *App) ListingOpen(ctx context.Context, input domain.ListingOpenInputV1, requestID string) (domain.ListingOpenOutputV1, error) {
	detail, observedAt, err := a.listingDetail(ctx, input.IDOrURL)
	if err != nil {
		return domain.ListingOpenOutputV1{}, err
	}
	if detail.Listing.URL == "" {
		return domain.ListingOpenOutputV1{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "listing detail did not contain a valid official public URL"}
	}
	data := map[string]string{"url": detail.Listing.URL}
	envelope := Envelope(a.Clock, "kcli.listing-open/v1", requestID, "mobile-api", data)
	envelope.ObservedAt = observedAt
	envelope.Warnings = detail.Warnings
	return domain.ListingOpenOutputV1{Envelope: envelope}, nil
}

func (a *App) listingDetail(ctx context.Context, reference string) (kleinanzeigen.ListingDetail, time.Time, error) {
	if a == nil || a.State == nil || a.Transport == nil || a.Clock == nil {
		return kleinanzeigen.ListingDetail{}, time.Time{}, &domain.Error{Code: domain.CodeUnavailable, Message: "listing runtime dependencies are unavailable"}
	}
	detail, err := kleinanzeigen.ListingFetch(ctx, a.Transport, reference)
	if err != nil {
		return kleinanzeigen.ListingDetail{}, time.Time{}, err
	}
	observedAt := a.Clock.Now().UTC()
	if detail.Seller.ID != "" && detail.Seller.Name != "" {
		foldedName := sellerFoldName(detail.Seller.Name)
		if !sellerIDPattern.MatchString(detail.Seller.ID) || foldedName == "" {
			detail.Warnings = append(detail.Warnings, domain.WarningV1{Code: "seller_index_skipped", Message: "listing seller identity could not be safely added to the local index"})
			return detail, observedAt, nil
		}
		public, err := json.Marshal(detail.Seller.Public)
		if err != nil {
			return kleinanzeigen.ListingDetail{}, time.Time{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "encode public seller snapshot", Cause: err}
		}
		seller := state.SellerSnapshot{ID: detail.Seller.ID, FoldedName: foldedName, DisplayName: detail.Seller.Name, PublicJSON: public, Source: "listing", Completeness: string(domain.CompletenessDirect), ObservedAt: observedAt}
		listing := state.SellerListingSnapshot{SellerID: detail.Seller.ID, ListingID: detail.Listing.ID, Title: detail.Listing.Title, Status: detail.Listing.Availability, URL: detail.Listing.URL, ObservedAt: observedAt}
		if err := a.State.UpsertSellerListing(ctx, seller, listing); err != nil {
			return kleinanzeigen.ListingDetail{}, time.Time{}, &domain.Error{Code: domain.CodeUnavailable, Message: "update local seller index", Cause: err}
		}
	}
	return detail, observedAt, nil
}

func listingImageOutput(mediaItems []kleinanzeigen.ListingMedia) []map[string]any {
	items := make([]map[string]any, 0, len(mediaItems))
	for _, item := range mediaItems {
		entry := map[string]any{"index": item.Index, "relation": item.Relation, "url": item.URL}
		if item.Width > 0 {
			entry["width"] = item.Width
		}
		if item.Height > 0 {
			entry["height"] = item.Height
		}
		if item.SizeLabel != "" {
			entry["size_label"] = item.SizeLabel
		}
		items = append(items, entry)
	}
	return items
}

func listingSelectMedia(items []kleinanzeigen.ListingMedia, selector string) ([]kleinanzeigen.ListingMedia, error) {
	if selector == "all" {
		if len(items) == 0 {
			return nil, &domain.Error{Code: domain.CodeNotFound, Message: "listing has no returned image variants"}
		}
		return append([]kleinanzeigen.ListingMedia(nil), items...), nil
	}
	selected := make([]kleinanzeigen.ListingMedia, 0)
	if index, err := strconv.Atoi(selector); err == nil && index >= 0 && strconv.Itoa(index) == selector {
		for _, item := range items {
			if item.Index == index {
				selected = append(selected, item)
			}
		}
	} else {
		if selector == "" || len(selector) > 128 {
			return nil, &domain.Error{Code: domain.CodeInvalidInput, Message: "download selector must be an image index, exact relation, or all"}
		}
		for _, item := range items {
			if item.Relation == selector {
				selected = append(selected, item)
			}
		}
	}
	if len(selected) == 0 {
		return nil, &domain.Error{Code: domain.CodeNotFound, Message: "download selector did not match a returned image variant", Details: map[string]any{"selector": selector}}
	}
	return selected, nil
}

func listingMediaKey(item kleinanzeigen.ListingMedia) string {
	return fmt.Sprintf("%d\x00%s", item.Index, item.Relation)
}
