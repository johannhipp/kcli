package output

import (
	"bufio"
	"io"

	"github.com/johannhipp/kcli/internal/domain"
)

// Project result rows without discarding the envelope metadata needed to
// interpret a finite stream. The summary always describes its source envelope.
func writeNDJSON(writer io.Writer, value any, fields []string) error {
	original, _ := value.(map[string]any)
	_, hasRows := original["data"].([]any)
	if len(fields) > 0 {
		projected, err := project(value, fields)
		if err != nil {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error()}
		}
		value = projected
	}
	buffered := bufio.NewWriter(writer)
	if !hasRows {
		if err := writeJSON(buffered, value, false); err != nil {
			return err
		}
		return buffered.Flush()
	}
	projected, _ := value.(map[string]any)
	rows, _ := projected["data"].([]any)
	for _, row := range rows {
		if err := writeJSON(buffered, row, false); err != nil {
			return err
		}
	}
	summary := map[string]any{"schema": domain.SummarySchemaV1, "returned": len(rows)}
	for _, key := range []string{"request_id", "observed_at", "source", "completeness", "page", "next", "warnings"} {
		if metadata, exists := original[key]; exists {
			summary[key] = metadata
		}
	}
	if err := writeJSON(buffered, summary, false); err != nil {
		return err
	}
	return buffered.Flush()
}
