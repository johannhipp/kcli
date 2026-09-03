package cli

import "github.com/johannhipp/kcli/internal/domain"

func (c *SellerGetCmd) Run(runtime *Runtime) error {
	if err := sellerRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.SellerGet(runtime.Context, domain.SellerGetInputV1{IDOrURL: c.IDOrURL, Listing: c.Listing}, runtime.RequestID)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *SellerSearchCmd) Run(runtime *Runtime) error {
	if err := sellerRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.SellerSearch(runtime.Context, domain.SellerSearchInputV1{Name: c.Name, Match: c.Match}, runtime.RequestID)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *SellerListingsCmd) Run(runtime *Runtime) error {
	if err := sellerRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.SellerListings(runtime.Context, domain.SellerListingsInputV1{IDOrURL: c.IDOrURL, Limit: c.Limit}, runtime.RequestID)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func sellerRuntimeReady(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil || runtime.Core.State == nil || runtime.Core.Clock == nil || runtime.Encoder.Format == "" || runtime.Stdout == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "seller runtime dependencies are unavailable"}
	}
	return nil
}
