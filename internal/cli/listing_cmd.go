package cli

import (
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/platform"
)

func (c *ListingGetCmd) Run(runtime *Runtime) error {
	if err := listingRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.ListingGet(runtime.Context, domain.ListingGetInputV1{IDOrURL: c.IDOrURL, Raw: c.Raw}, runtime.RequestID)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *ListingImagesCmd) Run(runtime *Runtime) error {
	if err := listingRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.ListingImages(runtime.Context, domain.ListingImagesInputV1{IDOrURL: c.IDOrURL, Download: c.Download, OutputDir: c.OutputDir, AllowOutsideCWD: c.AllowOutsideCWD, MaxBytes: c.MaxBytes, Overwrite: c.Overwrite}, runtime.RequestID)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *ListingOpenCmd) Run(runtime *Runtime) error {
	if err := listingRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.ListingOpen(runtime.Context, domain.ListingOpenInputV1{IDOrURL: c.IDOrURL}, runtime.RequestID)
	if err != nil {
		return err
	}
	if err := platform.OpenURL(runtime.Context, result.Data["url"]); err != nil {
		result.Warnings = append(result.Warnings, domain.WarningV1{Code: "desktop_open_unavailable", Message: "the official listing URL could not be opened on this desktop", Details: map[string]any{"error": err.Error()}})
		result.Completeness = domain.CompletenessPartial
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func listingRuntimeReady(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil || runtime.Core.Transport == nil || runtime.Core.State == nil || runtime.Core.Clock == nil || runtime.Encoder.Format == "" || runtime.Stdout == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "listing runtime dependencies are unavailable"}
	}
	return nil
}
