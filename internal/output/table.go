package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"
)

func writeTable(writer io.Writer, value any) (err error) {
	tw := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	defer func() {
		if flushErr := tw.Flush(); err == nil {
			err = flushErr
		}
	}()
	if object, ok := value.(map[string]any); ok {
		if data, exists := object["data"]; exists {
			value = data
		}
	}
	switch current := value.(type) {
	case []any:
		if len(current) == 0 {
			_, err := fmt.Fprintln(tw, "(no results)")
			return err
		}
		keys := collectKeys(current)
		if _, err := fmt.Fprintln(tw, strings.ToUpper(strings.Join(keys, "\t"))); err != nil {
			return err
		}
		for _, row := range current {
			object, _ := row.(map[string]any)
			values := make([]string, len(keys))
			for i, key := range keys {
				values[i] = human(object[key])
			}
			if _, err := fmt.Fprintln(tw, strings.Join(values, "\t")); err != nil {
				return err
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(tw, "%s\t%s\n", key, human(current[key])); err != nil {
				return err
			}
		}
	default:
		_, err := fmt.Fprintln(tw, human(current))
		return err
	}
	return nil
}
func collectKeys(rows []any) []string {
	set := map[string]bool{}
	for _, row := range rows {
		if object, ok := row.(map[string]any); ok {
			for key := range object {
				set[key] = true
			}
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func human(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return sanitizeHuman(text)
	}
	data, _ := json.Marshal(value)
	return sanitizeHuman(string(data))
}
func sanitizeHuman(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == 0x1b || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return ' '
		}
		return r
	}, value)
}
