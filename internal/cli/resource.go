package cli

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/domain"
)

type SearchCmd struct {
	Query           string   `arg:"" optional:"" help:"Keyword query."`
	Category        string   `help:"Category ID or path."`
	Location        string   `help:"Location ID or text."`
	Radius          *int     `help:"Radius in kilometres."`
	MinPrice        string   `name:"min-price" help:"Exact minimum euro amount."`
	MaxPrice        string   `name:"max-price" help:"Exact maximum euro amount."`
	AdType          *string  `name:"ad-type" enum:"offered,wanted" help:"Ad type (default: offered)."`
	PictureRequired bool     `name:"picture-required" help:"Require at least one picture."`
	Sort            *string  `enum:"date-desc,price-asc,price-desc,distance-asc" help:"Sort mode (default: date-desc)."`
	Filter          []string `help:"Dynamic KEY=VALUE filter."`
	Exclude         []string `help:"Exclude matching title or description text."`
	Page            *int     `help:"Zero-based page number."`
	PageSize        *int     `name:"page-size" help:"Results per page (default: 25)."`
	Paginate        bool     `help:"Fetch pages up to the result bound."`
	Limit           *int     `help:"Maximum results (default: 100 when paginating)."`
	Input           string   `type:"path" help:"Versioned JSON input file or -."`
}

func (c *SearchCmd) Validate() error {
	if c.Input != "" {
		hasBuilder := c.Query != "" || c.Category != "" || c.Location != "" || c.Radius != nil || c.MinPrice != "" || c.MaxPrice != "" || c.AdType != nil || c.PictureRequired || c.Sort != nil || len(c.Filter) > 0 || len(c.Exclude) > 0 || c.Page != nil || c.PageSize != nil || c.Paginate || c.Limit != nil
		if hasBuilder {
			return fmt.Errorf("--input is mutually exclusive with search-building arguments")
		}
		return nil
	}
	if c.Query != "" {
		if err := validateText(c.Query, 4096, "query"); err != nil {
			return err
		}
	}
	if c.Page != nil && *c.Page < 0 {
		return fmt.Errorf("page must not be negative")
	}
	if c.PageSize != nil && (*c.PageSize < 1 || *c.PageSize > 25) {
		return fmt.Errorf("page-size must be between 1 and 25")
	}
	if c.Limit != nil && (*c.Limit < 1 || *c.Limit > 1000) {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	for _, filter := range c.Filter {
		parts := strings.SplitN(filter, "=", 2)
		if len(parts) != 2 || validateReference(parts[0], false) != nil || parts[1] == "" {
			return fmt.Errorf("filters must use KEY=VALUE")
		}
	}
	for _, exclusion := range c.Exclude {
		if err := validateText(exclusion, 4096, "exclusion"); err != nil {
			return err
		}
	}
	return nil
}
func (c *SearchCmd) Describe() app.OperationMeta {
	return operation("Search listings with a reproducible bounded specification.", domain.SearchInputV1{}, domain.SearchOutputV1{}, "kcli.search-results/v1", false, app.SideEffectNone, false, app.EvidenceLive, 25, 1000, []string{"kcli search thinkpad --limit 25"}, []string{"V01-SEARCH-01", "V01-SEARCH-03", "V01-SEARCH-04", "V01-SEARCH-05", "V01-SEARCH-06", "V01-SEARCH-07", "V01-SEARCH-08", "V01-SEARCH-09", "V01-SEARCH-10"})
}

type CategoryListCmd struct {
	Refresh bool `help:"Refresh the tree from the server, ignoring the cache."`
}

func (*CategoryListCmd) Describe() app.OperationMeta {
	return operation("List the cached or refreshed category tree.", domain.CategoryListInputV1{}, domain.CategoryListOutputV1{}, "kcli.categories/v1", false, app.SideEffectLocal, false, app.EvidenceLive, 0, 0, []string{"kcli category list"}, []string{"V01-SEARCH-02"})
}

type CategoryGetCmd struct {
	IDOrPath string `arg:"" name:"id-or-path" help:"Category ID or slash-separated path."`
}

func (c *CategoryGetCmd) Validate() error { return validateReference(c.IDOrPath, true) }
func (*CategoryGetCmd) Describe() app.OperationMeta {
	return operation("Resolve one category by ID or path.", domain.CategoryGetInputV1{}, domain.CategoryOutputV1{}, "kcli.category/v1", false, app.SideEffectNone, false, app.EvidenceLive, 0, 0, []string{"kcli category get 278"}, []string{"V01-SEARCH-02"})
}

type CategorySearchCmd struct {
	Text string `arg:"" help:"Search text."`
}

func (c *CategorySearchCmd) Validate() error { return validateText(c.Text, 256, "category text") }
func (*CategorySearchCmd) Describe() app.OperationMeta {
	return operation("Search category labels and paths locally.", domain.CategorySearchInputV1{}, domain.CategoryListOutputV1{}, "kcli.categories/v1", false, app.SideEffectNone, false, app.EvidenceLocal, 25, 100, []string{"kcli category search fahrrad"}, []string{"V01-SEARCH-02"})
}

type LocationResolveCmd struct {
	Text  string `arg:"" help:"Place name or postcode."`
	Limit int    `default:"10" help:"Maximum candidates (default: 10)."`
}

func (c *LocationResolveCmd) Validate() error {
	if err := validateText(c.Text, 256, "location"); err != nil {
		return err
	}
	if c.Limit < 1 || c.Limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	return nil
}
func (*LocationResolveCmd) Describe() app.OperationMeta {
	return operation("Resolve a place or postcode to mobile location IDs.", domain.LocationResolveInputV1{}, domain.LocationOutputV1{}, "kcli.locations/v1", false, app.SideEffectLocal, false, app.EvidenceLive, 10, 100, []string{"kcli location resolve Berlin"}, []string{"V01-SEARCH-03"})
}

type FilterListCmd struct {
	Category string `required:"" help:"Category ID or path."`
	Refresh  bool   `help:"Refresh metadata from the server, ignoring the cache."`
}

func (c *FilterListCmd) Validate() error { return validateReference(c.Category, true) }
func (*FilterListCmd) Describe() app.OperationMeta {
	return operation("List dynamic filters advertised for a category.", domain.FilterListInputV1{}, domain.FilterListOutputV1{}, "kcli.filters/v1", false, app.SideEffectLocal, false, app.EvidenceSource, 0, 0, []string{"kcli filter list --category 278"}, []string{"V01-SEARCH-05", "V01-SEARCH-06"})
}

type FilterGetCmd struct {
	Category string `required:"" help:"Category ID or path."`
	Key      string `arg:"" help:"Filter metadata key."`
}

func (c *FilterGetCmd) Validate() error {
	if err := validateReference(c.Category, true); err != nil {
		return err
	}
	return validateReference(c.Key, false)
}
func (*FilterGetCmd) Describe() app.OperationMeta {
	return operation("Show one dynamic filter and its serialization proof.", domain.FilterGetInputV1{}, domain.FilterOutputV1{}, "kcli.filter/v1", false, app.SideEffectNone, false, app.EvidenceSource, 0, 0, []string{"kcli filter get --category 278 condition"}, []string{"V01-SEARCH-05", "V01-SEARCH-06"})
}

type ListingGetCmd struct {
	IDOrURL string `arg:"" name:"id-or-url" help:"Listing ID or public URL."`
	Raw     bool   `help:"Include the redacted raw response."`
}

func (c *ListingGetCmd) Validate() error { return validateListingReference(c.IDOrURL) }
func (*ListingGetCmd) Describe() app.OperationMeta {
	return operation("Get normalized listing details with optional redacted raw data.", domain.ListingGetInputV1{}, domain.ListingOutputV1{}, "kcli.listing/v1", false, app.SideEffectNone, false, app.EvidenceLive, 0, 0, []string{"kcli listing get 1234567890"}, []string{"V01-LISTING-01", "V01-LISTING-02", "V01-LISTING-05", "V01-LISTING-06", "V01-LISTING-07"})
}

type ListingImagesCmd struct {
	IDOrURL         string `arg:"" name:"id-or-url" help:"Listing ID or public URL."`
	Download        string `help:"Download selector: index, relation, or all."`
	OutputDir       string `name:"output-dir" type:"path" help:"Destination directory (default ./kcli-downloads)."`
	AllowOutsideCWD string `name:"allow-outside-cwd" type:"path" help:"Exact absolute path permitted outside the working directory."`
	MaxBytes        int64  `name:"max-bytes" default:"26214400" help:"Maximum bytes per image (default 26214400)."`
	Overwrite       bool   `help:"Overwrite an existing destination file."`
}

func (c *ListingImagesCmd) Validate() error {
	if err := validateListingReference(c.IDOrURL); err != nil {
		return err
	}
	if c.MaxBytes < 1 || c.MaxBytes > 25<<20 {
		return fmt.Errorf("max-bytes must be between 1 and 26214400")
	}
	return nil
}
func (*ListingImagesCmd) Describe() app.OperationMeta {
	return operation("List image variants or safely download selected variants.", domain.ListingImagesInputV1{}, domain.ListingImagesOutputV1{}, "kcli.listing-images/v1", false, app.SideEffectLocal, false, app.EvidenceLive, 0, 0, []string{"kcli listing images 1234567890"}, []string{"V01-LISTING-03", "V01-LISTING-04"})
}

type ListingOpenCmd struct {
	IDOrURL string `arg:"" name:"id-or-url" help:"Listing ID or public URL."`
}

func (c *ListingOpenCmd) Validate() error { return validateListingReference(c.IDOrURL) }
func (*ListingOpenCmd) Describe() app.OperationMeta {
	return operation("Open only the normalized official public listing URL.", domain.ListingOpenInputV1{}, domain.ListingOpenOutputV1{}, "kcli.listing-open/v1", false, app.SideEffectLocal, false, app.EvidenceLive, 0, 0, []string{"kcli listing open 1234567890"}, []string{"V01-LISTING-08"})
}

type SellerGetCmd struct {
	IDOrURL string `arg:"" optional:"" name:"id-or-url" help:"Seller ID or public profile/company URL."`
	Listing string `help:"Listing ID or URL to resolve the seller from."`
}

func (c *SellerGetCmd) Validate() error {
	if (c.IDOrURL == "") == (c.Listing == "") {
		return fmt.Errorf("provide exactly one seller reference or --listing")
	}
	if c.Listing != "" {
		return validateListingReference(c.Listing)
	}
	return validateSellerReference(c.IDOrURL)
}
func (*SellerGetCmd) Describe() app.OperationMeta {
	return operation("Get a seller from a known reference or listing.", domain.SellerGetInputV1{}, domain.SellerOutputV1{}, "kcli.seller/v1", false, app.SideEffectNone, false, app.EvidenceProvisional, 0, 0, []string{"kcli seller get --listing 1234567890"}, []string{"V01-USER-01", "V01-USER-02", "V01-USER-04", "V01-USER-05"})
}

type SellerSearchCmd struct {
	Name  string `arg:"" help:"Seller display name to search."`
	Match string `enum:"exact,contains" default:"contains" help:"Match mode: exact or contains (default: contains)."`
}

func (c *SellerSearchCmd) Validate() error { return validateText(c.Name, 256, "seller name") }
func (*SellerSearchCmd) Describe() app.OperationMeta {
	return operation("Search the bounded local seller index by name.", domain.SellerSearchInputV1{}, domain.SellerListOutputV1{}, "kcli.sellers/v1", false, app.SideEffectNone, false, app.EvidenceLocal, 25, 100, []string{"kcli seller search Example"}, []string{"V01-USER-03", "V01-USER-05"})
}

type SellerListingsCmd struct {
	IDOrURL string `arg:"" name:"id-or-url" help:"Seller ID or public profile/company URL."`
	Limit   int    `default:"25" help:"Maximum listings (default: 25)."`
}

func (c *SellerListingsCmd) Validate() error {
	if err := validateSellerReference(c.IDOrURL); err != nil {
		return err
	}
	if c.Limit < 1 || c.Limit > 1000 {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	return nil
}
func (*SellerListingsCmd) Describe() app.OperationMeta {
	return operation("List only locally known listings for a seller.", domain.SellerListingsInputV1{}, domain.SellerListingsOutputV1{}, "kcli.seller-listings/v1", false, app.SideEffectNone, false, app.EvidenceLocal, 25, 1000, []string{"kcli seller listings 123"}, []string{"V01-USER-04", "V01-USER-05"})
}

type AuthLoginCmd struct {
	NoOpen       bool   `name:"no-open" help:"Do not open the browser; require --redirect-file."`
	RedirectFile string `name:"redirect-file" type:"path" help:"Read the callback redirect URL from a file, or - for stdin."`
}

func (*AuthLoginCmd) Describe() app.OperationMeta {
	return operation("Log in through interactive Auth0 PKCE without accepting a password.", domain.AuthLoginInputV1{}, domain.AuthOutputV1{}, "kcli.auth/v1", false, app.SideEffectLocal, false, app.EvidenceSource, 0, 0, []string{"kcli auth login --no-open --redirect-file -"}, []string{"V01-DM-01"})
}

type AuthStatusCmd struct{ Check bool }

func (*AuthStatusCmd) Describe() app.OperationMeta {
	return operation("Show local session status with an optional remote check.", domain.AuthStatusInputV1{}, domain.AuthOutputV1{}, "kcli.auth/v1", false, app.SideEffectNone, false, app.EvidenceSource, 0, 0, []string{"kcli auth status"}, []string{"V01-DM-01"})
}

type AuthLogoutCmd struct {
	DryRun bool `name:"dry-run" help:"Preview local session effects without clearing them."`
}

func (*AuthLogoutCmd) Describe() app.OperationMeta {
	return operation("Preview or clear only the local authenticated session.", domain.AuthLogoutInputV1{}, domain.AuthOutputV1{}, "kcli.auth/v1", false, app.SideEffectLocal, false, app.EvidenceSource, 0, 0, []string{"kcli auth logout --dry-run"}, []string{"V01-DM-01"})
}

type DMListCmd struct {
	Unread   bool `help:"Only conversations with unread messages."`
	Page     int  `default:"0" help:"Zero-based page number."`
	PageSize int  `name:"page-size" default:"50" help:"Conversations per page (default: 50)."`
	Paginate bool `help:"Fetch pages up to the result bound."`
	Limit    int  `default:"50" help:"Maximum conversations (default: 50)."`
}

func (c *DMListCmd) Validate() error {
	if c.Page < 0 || c.PageSize < 1 || c.PageSize > 100 || c.Limit < 1 || c.Limit > 500 {
		return fmt.Errorf("invalid page, page-size, or limit")
	}
	return nil
}
func (*DMListCmd) Describe() app.OperationMeta {
	return operation("List bounded conversation summaries newest first.", domain.DMListInputV1{}, domain.DMListOutputV1{}, "kcli.conversations/v1", true, app.SideEffectNone, false, app.EvidenceSource, 50, 500, []string{"kcli dm list --unread"}, []string{"V01-DM-02"})
}

type DMGetCmd struct {
	ConversationID string `arg:"" name:"conversation-id" help:"Conversation to read."`
}

func (c *DMGetCmd) Validate() error { return validateReference(c.ConversationID, false) }
func (*DMGetCmd) Describe() app.OperationMeta {
	return operation("Read available conversation history oldest first.", domain.DMGetInputV1{}, domain.DMOutputV1{}, "kcli.conversation/v1", true, app.SideEffectAccountState, false, app.EvidenceSource, 0, 0, []string{"kcli dm get 123"}, []string{"V01-DM-03", "V01-DM-07"})
}

type DMMarkReadCmd struct {
	ConversationIDs []string `arg:"" optional:"" name:"conversation-id" help:"One or more conversation IDs to mark read."`
	Input           string   `type:"path" help:"Versioned JSON input file or - with conversation_ids."`
	DryRun          bool     `name:"dry-run" help:"Preview mark-read without changing account state."`
}

func (c *DMMarkReadCmd) Validate() error {
	if c.Input != "" {
		if len(c.ConversationIDs) > 0 {
			return fmt.Errorf("provide conversation IDs either positionally or via --input, not both")
		}
		return nil
	}
	if len(c.ConversationIDs) == 0 || len(c.ConversationIDs) > 100 {
		return fmt.Errorf("provide between 1 and 100 conversation IDs")
	}
	for _, id := range c.ConversationIDs {
		if err := validateReference(id, false); err != nil {
			return err
		}
	}
	return nil
}
func (*DMMarkReadCmd) Describe() app.OperationMeta {
	return operation("Preview or mark bounded conversations read.", domain.DMMarkReadInputV1{}, domain.DMOutputV1{}, "kcli.dm-mark-read/v1", true, app.SideEffectAccountState, false, app.EvidenceSource, 0, 100, []string{"kcli dm mark-read 123 --dry-run"}, []string{"V01-DM-06"})
}

type DMPollCmd struct {
	After       string `help:"Resume after this cursor."`
	Since       string `help:"Baseline time (RFC 3339) or now."`
	Limit       int    `default:"200" help:"Conversations to scan this cycle (default: 200)."`
	Advance     bool   `help:"Advance the stored cursor after a successful cycle."`
	NoAdvance   bool   `name:"no-advance" help:"Never advance the stored cursor."`
	OpenChanged bool   `name:"open-changed" help:"Open changed threads (state-touching) to identify message events."`
}

func (c *DMPollCmd) Validate() error {
	if c.After != "" && c.Since != "" {
		return fmt.Errorf("--after and --since are mutually exclusive")
	}
	if c.Advance && c.NoAdvance {
		return fmt.Errorf("--advance and --no-advance are mutually exclusive")
	}
	if c.Limit < 1 || c.Limit > 500 {
		return fmt.Errorf("limit must be between 1 and 500")
	}
	return nil
}
func (*DMPollCmd) Describe() app.OperationMeta {
	return operation("Run one restart-safe incremental DM synchronization cycle.", domain.DMPollInputV1{}, domain.SyncOutputV1{}, "kcli.dm-events/v1", true, app.SideEffectAccountState, false, app.EvidenceProvisional, 200, 500, []string{"kcli dm poll --since now"}, []string{"V01-DM-09"})
}

type DMWatchCmd struct {
	After             string        `help:"Resume after this cursor."`
	Since             string        `help:"Baseline time (RFC 3339) or now."`
	Interval          time.Duration `default:"30s" help:"Sync interval (minimum 30s)."`
	Limit             int           `default:"200" help:"Conversations to scan per cycle (default: 200)."`
	IncludeHeartbeats bool          `name:"include-heartbeats" help:"Emit a heartbeat when the stream is idle."`
	OpenChanged       bool          `name:"open-changed" help:"Open changed threads (state-touching) to identify message events."`
}

func (c *DMWatchCmd) Validate() error {
	if c.After != "" && c.Since != "" {
		return fmt.Errorf("--after and --since are mutually exclusive")
	}
	if c.Interval < 30*time.Second {
		return fmt.Errorf("interval must be at least 30s")
	}
	if c.Limit < 1 || c.Limit > 500 {
		return fmt.Errorf("limit must be between 1 and 500")
	}
	return nil
}
func (*DMWatchCmd) Describe() app.OperationMeta {
	return operation("Watch DM changes as NDJSON synthesized from conservative polling.", domain.DMWatchInputV1{}, domain.EventV1{}, "kcli.event/v1", true, app.SideEffectAccountState, false, app.EvidenceProvisional, 200, 500, []string{"kcli dm watch --since now"}, []string{"V01-DM-10"})
}

type DMReplyCmd struct {
	ConversationID               string `arg:"" name:"conversation-id" help:"Conversation to reply in."`
	Message                      string `help:"Message text."`
	MessageFile                  string `name:"message-file" type:"path" help:"Read message text from a file, or - for stdin."`
	Input                        string `type:"path" help:"Versioned JSON operation input file or -."`
	DryRun                       bool   `name:"dry-run" help:"Preview the reply plan without sending."`
	Confirm                      string `help:"Confirmation id from a matching dry run."`
	AcknowledgeWarning           string `name:"acknowledge-warning" help:"Acknowledge a platform warning code."`
	AcknowledgePossibleDuplicate string `name:"acknowledge-possible-duplicate" help:"Acknowledge an ambiguous prior send by its confirmation id."`
}

func (c *DMReplyCmd) Validate() error {
	if err := validateReference(c.ConversationID, false); err != nil {
		return err
	}
	return validateMutationInput(c.Message, c.MessageFile, c.Input, c.DryRun, c.Confirm)
}
func (*DMReplyCmd) Describe() app.OperationMeta {
	return operation("Preview or single-claim-confirm one external text reply.", domain.DMReplyInputV1{}, domain.DMOutputV1{}, "kcli.dm-reply/v1", true, app.SideEffectExternalMessage, true, app.EvidenceSource, 0, 0, []string{"kcli dm reply 123 --message-file note.txt --dry-run"}, []string{"V01-DM-04", "V01-DM-08"})
}

type DMStartCmd struct {
	ListingIDOrURL               string `arg:"" name:"listing-id-or-url" help:"Listing ID or public URL to contact."`
	Message                      string `help:"First message text."`
	MessageFile                  string `name:"message-file" type:"path" help:"Read message text from a file, or - for stdin."`
	Input                        string `type:"path" help:"Versioned JSON operation input file or -."`
	ContactName                  string `name:"contact-name" help:"Exact contact name sent with the first message."`
	DryRun                       bool   `name:"dry-run" help:"Preview the first-contact plan without sending."`
	Confirm                      string `help:"Confirmation id from a matching dry run."`
	AcknowledgeWarning           string `name:"acknowledge-warning" help:"Acknowledge a platform warning code."`
	AcknowledgePossibleDuplicate string `name:"acknowledge-possible-duplicate" help:"Acknowledge an ambiguous prior send by its confirmation id."`
}

func (c *DMStartCmd) Validate() error {
	if err := validateListingReference(c.ListingIDOrURL); err != nil {
		return err
	}
	return validateMutationInput(c.Message, c.MessageFile, c.Input, c.DryRun, c.Confirm)
}
func (*DMStartCmd) Describe() app.OperationMeta {
	return operation("Preview or single-claim-confirm one first-contact message.", domain.DMStartInputV1{}, domain.DMOutputV1{}, "kcli.dm-start/v1", true, app.SideEffectExternalMessage, true, app.EvidenceSource, 0, 0, []string{"kcli dm start 123 --message-file note.txt --dry-run"}, []string{"V01-DM-05", "V01-DM-08"})
}

func operation(purpose string, input, outputType any, schema string, auth bool, side app.SideEffect, confirmation bool, evidence app.Evidence, defaultLimit, hardLimit int, examples, stories []string) app.OperationMeta {
	inputType, outputReflect := reflect.TypeOf(input), reflect.TypeOf(outputType)
	return app.OperationMeta{Purpose: purpose, InputName: inputType.Name(), OutputName: outputReflect.Name(), SchemaVersion: schema, AuthRequired: auth, SideEffect: side, ConfirmationRequired: confirmation, Evidence: evidence, DefaultLimit: defaultLimit, HardLimit: hardLimit, Examples: examples, StoryIDs: stories, InputType: inputType, OutputType: outputReflect}
}
func validateText(value string, max int, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if !utf8.ValidString(value) || len(value) > max || strings.IndexFunc(value, func(r rune) bool { return r == 0 || r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("%s contains invalid text", label)
	}
	return nil
}
func validateReference(value string, allowSlash bool) error {
	if value == "" || len(value) > 512 || !utf8.ValidString(value) || strings.ContainsAny(value, "?#\x00") {
		return fmt.Errorf("invalid identifier")
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") || strings.Contains(value, "..") || (!allowSlash && strings.ContainsAny(value, "/\\")) || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("invalid identifier")
	}
	return nil
}

var listingPath = regexp.MustCompile(`/[0-9]+-[0-9]+-[0-9]+/?$`)

func validateListingReference(value string) error {
	if !strings.Contains(value, "://") {
		return validateReference(value, false)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Hostname() != "www.kleinanzeigen.de" && parsed.Hostname() != "kleinanzeigen.de") || !listingPath.MatchString(parsed.Path) {
		return fmt.Errorf("invalid listing URL")
	}
	return nil
}
func validateSellerReference(value string) error {
	if !strings.Contains(value, "://") {
		return validateReference(value, false)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Hostname() != "www.kleinanzeigen.de" && parsed.Hostname() != "kleinanzeigen.de") || parsed.Path == "" || strings.Contains(value, "..") {
		return fmt.Errorf("invalid seller reference")
	}
	return nil
}
func validateMutationInput(message, messageFile, input string, dryRun bool, confirm string) error {
	count := 0
	for _, value := range []string{message, messageFile, input} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("provide exactly one of --message, --message-file, or --input")
	}
	if dryRun == (confirm != "") {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if message != "" && (!utf8.ValidString(message) || len(message) > 65536 || strings.IndexByte(message, 0) >= 0) {
		return fmt.Errorf("invalid message text")
	}
	if confirm != "" {
		return validateReference(confirm, false)
	}
	return nil
}
