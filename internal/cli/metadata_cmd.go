package cli

import (
	"fmt"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

func (c *CategoryListCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.Categories(runtime.Context, c.Refresh)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.categories/v1", result)
}

func (c *CategoryGetCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.Category(runtime.Context, c.IDOrPath)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.category/v1", result)
}

func (c *CategorySearchCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.SearchCategories(runtime.Context, c.Text)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.categories/v1", result)
}

func (c *LocationResolveCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.Locations(runtime.Context, c.Text, c.Limit)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.locations/v1", result)
}

func (c *FilterListCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.Filters(runtime.Context, c.Category, c.Refresh)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.filters/v1", result)
}

func (c *FilterGetCmd) Run(runtime *Runtime) error {
	service, err := metadataService(runtime)
	if err != nil {
		return err
	}
	result, err := service.Filter(runtime.Context, c.Category, c.Key)
	if err != nil {
		return err
	}
	return emitMetadata(runtime, "kcli.filter/v1", result)
}

func metadataService(runtime *Runtime) (*kleinanzeigen.MetadataService, error) {
	if runtime == nil || runtime.Core == nil || runtime.Core.State == nil || runtime.Core.Transport == nil {
		return nil, fmt.Errorf("metadata runtime dependencies are unavailable")
	}
	return kleinanzeigen.NewMetadataService(runtime.Core.Transport, runtime.Core.State), nil
}

func emitMetadata[T any](runtime *Runtime, schema string, result kleinanzeigen.MetadataResult[T]) error {
	envelope := app.Envelope(runtime.Core.Clock, schema, runtime.RequestID, result.Source, result.Data)
	envelope.ObservedAt = result.ObservedAt
	envelope.Completeness = result.Completeness
	envelope.Warnings = result.Warnings
	envelope.Raw = result.Raw
	if envelope.Warnings == nil {
		envelope.Warnings = []domain.WarningV1{}
	}
	return runtime.Encoder.Encode(runtime.Stdout, envelope)
}
