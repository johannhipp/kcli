package domain

type FilterValueV1 struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SearchInputV1 struct {
	Schema          string          `json:"schema,omitempty"`
	Query           string          `json:"query,omitempty"`
	Category        string          `json:"category,omitempty"`
	Location        string          `json:"location,omitempty"`
	Radius          int             `json:"radius,omitempty"`
	MinPrice        string          `json:"min_price,omitempty"`
	MaxPrice        string          `json:"max_price,omitempty"`
	AdType          string          `json:"ad_type,omitempty"`
	PictureRequired bool            `json:"picture_required,omitempty"`
	Sort            string          `json:"sort,omitempty"`
	Filters         []FilterValueV1 `json:"filters,omitempty"`
	Exclusions      []string        `json:"exclusions,omitempty"`
	Page            int             `json:"page,omitempty"`
	PageSize        int             `json:"page_size,omitempty"`
	Paginate        bool            `json:"paginate,omitempty"`
	Limit           int             `json:"limit,omitempty"`
}

type CategoryListInputV1 struct {
	Refresh bool `json:"refresh,omitempty"`
}
type CategoryGetInputV1 struct {
	IDOrPath string `json:"id_or_path"`
}
type CategorySearchInputV1 struct {
	Text string `json:"text"`
}
type LocationResolveInputV1 struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"`
}
type FilterListInputV1 struct {
	Category string `json:"category"`
	Refresh  bool   `json:"refresh,omitempty"`
}
type FilterGetInputV1 struct {
	Category string `json:"category"`
	Key      string `json:"key"`
}
type ListingGetInputV1 struct {
	IDOrURL string `json:"id_or_url"`
	Raw     bool   `json:"raw,omitempty"`
}
type ListingImagesInputV1 struct {
	IDOrURL         string `json:"id_or_url"`
	Download        string `json:"download,omitempty"`
	OutputDir       string `json:"output_dir,omitempty"`
	AllowOutsideCWD string `json:"allow_outside_cwd,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	Overwrite       bool   `json:"overwrite,omitempty"`
}
type ListingOpenInputV1 struct {
	IDOrURL string `json:"id_or_url"`
}
type SellerGetInputV1 struct {
	IDOrURL string `json:"id_or_url,omitempty"`
	Listing string `json:"listing,omitempty"`
}
type SellerSearchInputV1 struct {
	Name  string `json:"name"`
	Match string `json:"match,omitempty"`
}
type SellerListingsInputV1 struct {
	IDOrURL string `json:"id_or_url"`
	Limit   int    `json:"limit,omitempty"`
}
type AuthLoginInputV1 struct {
	NoOpen       bool   `json:"no_open,omitempty"`
	RedirectFile string `json:"redirect_file,omitempty"`
}
type AuthStatusInputV1 struct {
	Check bool `json:"check,omitempty"`
}
type AuthLogoutInputV1 struct {
	DryRun bool `json:"dry_run,omitempty"`
}
type DMListInputV1 struct {
	Unread   bool `json:"unread,omitempty"`
	Page     int  `json:"page,omitempty"`
	PageSize int  `json:"page_size,omitempty"`
	Paginate bool `json:"paginate,omitempty"`
	Limit    int  `json:"limit,omitempty"`
}
type DMGetInputV1 struct {
	ConversationID string `json:"conversation_id"`
}
type DMMarkReadInputV1 struct {
	ConversationIDs []string `json:"conversation_ids"`
	DryRun          bool     `json:"dry_run,omitempty"`
}
type DMPollInputV1 struct {
	After       string `json:"after,omitempty"`
	Since       string `json:"since,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Advance     *bool  `json:"advance,omitempty"`
	OpenChanged bool   `json:"open_changed,omitempty"`
}
type DMWatchInputV1 struct {
	After             string `json:"after,omitempty"`
	Since             string `json:"since,omitempty"`
	Interval          string `json:"interval,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	IncludeHeartbeats bool   `json:"include_heartbeats,omitempty"`
	OpenChanged       bool   `json:"open_changed,omitempty"`
}
type DMReplyInputV1 struct {
	ConversationID               string `json:"conversation_id"`
	Message                      string `json:"message,omitempty"`
	MessageFile                  string `json:"message_file,omitempty"`
	Input                        string `json:"input,omitempty"`
	DryRun                       bool   `json:"dry_run,omitempty"`
	Confirm                      string `json:"confirm,omitempty"`
	AcknowledgeWarning           string `json:"acknowledge_warning,omitempty"`
	AcknowledgePossibleDuplicate string `json:"acknowledge_possible_duplicate,omitempty"`
}
type DMStartInputV1 struct {
	ListingIDOrURL               string `json:"listing_id_or_url"`
	Message                      string `json:"message,omitempty"`
	MessageFile                  string `json:"message_file,omitempty"`
	Input                        string `json:"input,omitempty"`
	ContactName                  string `json:"contact_name,omitempty"`
	DryRun                       bool   `json:"dry_run,omitempty"`
	Confirm                      string `json:"confirm,omitempty"`
	AcknowledgeWarning           string `json:"acknowledge_warning,omitempty"`
	AcknowledgePossibleDuplicate string `json:"acknowledge_possible_duplicate,omitempty"`
}
type SchemaListInputV1 struct{}
type SchemaShowInputV1 struct {
	Command []string `json:"command"`
}
type SchemaFiltersInputV1 struct {
	Category string `json:"category"`
}
type ConfigListInputV1 struct{}
type ConfigGetInputV1 struct {
	Key string `json:"key"`
}
type ConfigSetInputV1 struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	DryRun bool   `json:"dry_run,omitempty"`
}
type ConfigPathInputV1 struct{}
type DoctorInputV1 struct {
	Network bool `json:"network,omitempty"`
	Auth    bool `json:"auth,omitempty"`
}
type CompletionInputV1 struct {
	Shell string `json:"shell"`
}
type VersionInputV1 struct{}
