package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
)

const (
	EventSchemaV1   = "kcli.event/v1"
	SummarySchemaV1 = "kcli.summary/v1"
	CursorFormatV1  = 1
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type CursorPayloadV1 struct {
	FormatVersion      int    `json:"v"`
	ProfileUUID        string `json:"profile_uuid"`
	AccountSubjectHash string `json:"account_subject_hash"`
	Generation         int64  `json:"generation"`
	Sequence           int64  `json:"sequence"`
}

func (p CursorPayloadV1) Validate() error {
	if p.FormatVersion != CursorFormatV1 {
		return fmt.Errorf("unsupported cursor format version %d", p.FormatVersion)
	}
	if !uuidPattern.MatchString(p.ProfileUUID) {
		return fmt.Errorf("invalid cursor profile UUID")
	}
	if !hashPattern.MatchString(p.AccountSubjectHash) {
		return fmt.Errorf("invalid cursor account subject hash")
	}
	if p.Generation < 0 || p.Sequence < 0 {
		return fmt.Errorf("invalid cursor position")
	}
	return nil
}

func EncodeCursor(p CursorPayloadV1) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return "cur_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodeCursor(s string) (CursorPayloadV1, error) {
	var p CursorPayloadV1
	if len(s) < 5 || s[:4] != "cur_" {
		return p, fmt.Errorf("cursor must begin with cur_")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s[4:])
	if err != nil {
		return p, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	if len(b) > 1024 {
		return p, fmt.Errorf("cursor payload is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("invalid cursor payload: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return p, fmt.Errorf("cursor contains trailing data")
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	return p, nil
}

type EventV1 struct {
	Schema         string         `json:"schema"`
	EventID        string         `json:"event_id"`
	Cursor         string         `json:"cursor"`
	Type           string         `json:"type"`
	ObservedAt     time.Time      `json:"observed_at"`
	AccountID      string         `json:"account_id"`
	ConversationID string         `json:"conversation_id,omitempty"`
	ListingID      string         `json:"listing_id,omitempty"`
	Data           map[string]any `json:"data"`
}

type SummaryV1 struct {
	Schema     string      `json:"schema"`
	RequestID  string      `json:"request_id"`
	ObservedAt time.Time   `json:"observed_at"`
	Returned   int         `json:"returned"`
	Next       *string     `json:"next"`
	Warnings   []WarningV1 `json:"warnings"`
}

type SyncOutputV1 struct {
	Envelope[[]EventV1]
	Cursor string `json:"cursor"`
}
