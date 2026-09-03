package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

const searchInputLimit = 1 << 20

func (c *SearchCmd) Run(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "search runtime dependencies are unavailable"}
	}
	input, err := searchCommandInput(runtime, c)
	if err != nil {
		return err
	}
	result, err := runtime.Core.Search(runtime.Context, input)
	if err != nil {
		return err
	}
	result.RequestID = runtime.RequestID
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func searchCommandInput(runtime *Runtime, command *SearchCmd) (domain.SearchInputV1, error) {
	if command.Input != "" {
		input, err := searchReadInput(command.Input, runtime.Stdin)
		if err != nil {
			return domain.SearchInputV1{}, err
		}
		if input.Schema != "kcli.search-input/v1" {
			return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidSchema, Message: "search input schema must be kcli.search-input/v1"}
		}
		return input, nil
	}
	input := domain.SearchInputV1{
		Query:           command.Query,
		Category:        command.Category,
		Location:        command.Location,
		MinPrice:        command.MinPrice,
		MaxPrice:        command.MaxPrice,
		PictureRequired: command.PictureRequired,
		Paginate:        command.Paginate,
		Exclusions:      append([]string(nil), command.Exclude...),
	}
	if command.Radius != nil {
		input.Radius = *command.Radius
	}
	if command.AdType != nil {
		input.AdType = *command.AdType
	}
	if command.Sort != nil {
		input.Sort = *command.Sort
	}
	if command.Page != nil {
		input.Page = *command.Page
	}
	if command.PageSize != nil {
		input.PageSize = *command.PageSize
	}
	if command.Limit != nil {
		input.Limit = *command.Limit
	}
	for _, raw := range command.Filter {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "filters must use KEY=VALUE"}
		}
		input.Filters = append(input.Filters, domain.FilterValueV1{Key: parts[0], Value: parts[1]})
	}
	return input, nil
}

func searchReadInput(path string, stdin io.Reader) (domain.SearchInputV1, error) {
	var reader io.Reader
	var closeInput func() error
	if path == "-" {
		reader = stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "open search input", Cause: err}
		}
		reader = file
		closeInput = file.Close
	}
	if closeInput != nil {
		defer closeInput()
	}
	body, err := io.ReadAll(io.LimitReader(reader, searchInputLimit+1))
	if err != nil {
		return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "read search input", Cause: err}
	}
	if len(body) > searchInputLimit {
		return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: fmt.Sprintf("search input exceeds %d bytes", searchInputLimit)}
	}
	var input domain.SearchInputV1
	if err := kleinanzeigen.DecodeUniqueJSON(body, &input); err != nil {
		return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode search input", Cause: err}
	}
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&input); err != nil {
		return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode search input", Cause: err}
	}
	if err := strict.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return domain.SearchInputV1{}, &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode search input", Cause: err}
	}
	return input, nil
}
