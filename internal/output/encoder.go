package output

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/johannhipp/kcli/internal/domain"
	"golang.org/x/term"
)

type Format string

const (
	FormatTable  Format = "table"
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
	FormatRaw    Format = "raw"
)

func ParseFormat(value string) (Format, error) {
	format := Format(value)
	switch format {
	case FormatTable, FormatJSON, FormatNDJSON, FormatRaw:
		return format, nil
	default:
		return "", fmt.Errorf("output must be table, json, ndjson, or raw")
	}
}

type fdWriter interface{ Fd() uintptr }

func IsTTY(writer io.Writer) bool {
	file, ok := writer.(fdWriter)
	return ok && term.IsTerminal(int(file.Fd()))
}

func NewRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req_%x", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(b[:])
}

type Encoder struct {
	Format Format
	Fields []string
}

func (e Encoder) Encode(writer io.Writer, value any) error {
	normalized, err := normalize(value)
	if err != nil {
		return err
	}
	if len(e.Fields) > 0 {
		normalized, err = project(normalized, e.Fields)
		if err != nil {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error()}
		}
	}
	switch e.Format {
	case FormatJSON:
		return writeJSON(writer, normalized, true)
	case FormatRaw:
		return writeJSON(writer, Redact(normalized), false)
	case FormatNDJSON:
		return writeNDJSON(writer, normalized)
	case FormatTable:
		return writeTable(writer, normalized)
	default:
		return fmt.Errorf("unknown output format %q", e.Format)
	}
}

func (e Encoder) EncodeError(writer io.Writer, err error, requestID string) error {
	de := &domain.Error{Code: "internal", Message: "operation failed"}
	var candidate *domain.Error
	if errors.As(err, &candidate) {
		de = candidate
	}
	retryAfter := ""
	if de.RetryAfter != nil {
		retryAfter = de.RetryAfter.String()
	}
	envelope := domain.ErrorEnvelopeV1{Schema: "kcli.error/v1", Code: de.Code, Message: de.Message, Retryable: de.Retryable, RequestID: requestID, RetryAfter: retryAfter, Details: redactMap(de.Details)}
	format := e.Format
	if format == FormatTable {
		format = FormatJSON
	}
	return Encoder{Format: format}.Encode(writer, envelope)
}

func normalize(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("normalize output: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var result any
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("normalize output: %w", err)
	}
	return result, nil
}

func writeJSON(writer io.Writer, value any, indent bool) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}
func writeNDJSON(writer io.Writer, value any) error {
	buffered := bufio.NewWriter(writer)
	object, ok := value.(map[string]any)
	if !ok {
		if err := writeJSON(buffered, value, false); err != nil {
			return err
		}
		return buffered.Flush()
	}
	rows, hasRows := object["data"].([]any)
	if !hasRows {
		if err := writeJSON(buffered, value, false); err != nil {
			return err
		}
		return buffered.Flush()
	}
	for _, row := range rows {
		if err := writeJSON(buffered, row, false); err != nil {
			return err
		}
	}
	summary := map[string]any{"schema": domain.SummarySchemaV1, "request_id": object["request_id"], "observed_at": object["observed_at"], "returned": len(rows), "next": object["next"], "warnings": object["warnings"]}
	if err := writeJSON(buffered, summary, false); err != nil {
		return err
	}
	return buffered.Flush()
}
func writeTable(writer io.Writer, value any) error {
	tw := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	defer tw.Flush()
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

func project(value any, fields []string) (any, error) {
	tree := map[string]any{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, fmt.Errorf("field paths cannot be empty")
		}
		node := tree
		parts := strings.Split(field, ".")
		for i, part := range parts {
			if part == "" {
				return nil, fmt.Errorf("invalid field path %q", field)
			}
			if i == len(parts)-1 {
				node[part] = true
				continue
			}
			next, ok := node[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[part] = next
			}
			node = next
		}
	}
	return projectNode(value, tree, "")
}
func projectNode(value any, tree map[string]any, prefix string) (any, error) {
	switch current := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range tree {
			found, ok := current[key]
			if !ok {
				return nil, fmt.Errorf("unknown field path %q", strings.TrimPrefix(prefix+"."+key, "."))
			}
			if childTree, ok := child.(map[string]any); ok {
				projected, err := projectNode(found, childTree, prefix+"."+key)
				if err != nil {
					return nil, err
				}
				out[key] = projected
			} else {
				out[key] = found
			}
		}
		return out, nil
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			projected, err := projectNode(item, tree, prefix)
			if err != nil {
				return nil, err
			}
			out[i] = projected
		}
		return out, nil
	default:
		return nil, fmt.Errorf("field path %q does not name an object", strings.TrimPrefix(prefix, "."))
	}
}

var emailPattern = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+(?:@|%40)[A-Z0-9.-]+(?:\.|%2e)[A-Z]{2,}`)
var sensitiveKeys = map[string]bool{"authorization": true, "access_token": true, "refresh_token": true, "id_token": true, "email": true, "message": true, "message_body": true, "password": true, "client_secret": true, "authorization_code": true, "code_verifier": true, "oauth_state": true}

func Redact(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			if sensitiveKeys[strings.ToLower(key)] {
				out[key] = "[REDACTED]"
			} else {
				out[key] = Redact(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, child := range current {
			out[i] = Redact(child)
		}
		return out
	case string:
		return redactString(current)
	default:
		return current
	}
}
func redactString(value string) string {
	if strings.HasPrefix(strings.ToLower(value), "bearer ") || strings.HasPrefix(strings.ToLower(value), "basic ") {
		return "[REDACTED]"
	}
	value = emailPattern.ReplaceAllString(value, "[REDACTED_EMAIL]")
	if !strings.Contains(value, "?") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if sensitiveKeys[strings.ToLower(key)] {
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return value
}
func redactMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	redacted, _ := Redact(value).(map[string]any)
	return redacted
}
