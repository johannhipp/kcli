package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/buildinfo"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/platform"
	schemacatalog "github.com/johannhipp/kcli/internal/schema"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
	kongcompletion "github.com/jotaen/kong-completion"
)

type Root struct {
	Profile string        `help:"Named profile." default:"default"`
	Output  string        `help:"Output format: table, json, ndjson, or raw." placeholder:"FORMAT"`
	Fields  string        `help:"Comma-separated schema field paths." placeholder:"LIST"`
	Quiet   bool          `help:"Suppress diagnostics, never primary output."`
	Debug   bool          `help:"Print redacted diagnostics."`
	Timeout time.Duration `help:"Lower the operation deadline." placeholder:"DURATION"`

	Search     SearchCmd     `cmd:"" help:"Search listings."`
	Category   CategoryCmd   `cmd:"" help:"Inspect categories."`
	Location   LocationCmd   `cmd:"" help:"Resolve locations."`
	Filter     FilterCmd     `cmd:"" help:"Inspect category search filters."`
	Listing    ListingCmd    `cmd:"" help:"Inspect listings and images."`
	Seller     SellerCmd     `cmd:"" help:"Inspect locally encountered sellers."`
	Auth       AuthCmd       `cmd:"" help:"Manage the local authenticated session."`
	DM         DMCmd         `cmd:"" name:"dm" help:"Read, synchronize, and communicate through direct messages."`
	Schema     SchemaCmd     `cmd:"" help:"Inspect machine-readable command schemas."`
	Config     ConfigCmd     `cmd:"" help:"Inspect and edit local configuration."`
	Doctor     DoctorCmd     `cmd:"" help:"Run bounded local diagnostics."`
	Completion CompletionCmd `cmd:"" help:"Print shell completion initialization code."`
	Version    VersionCmd    `cmd:"" help:"Print build version information."`
}

type CategoryCmd struct {
	List   CategoryListCmd   `cmd:"" help:"List categories."`
	Get    CategoryGetCmd    `cmd:"" help:"Get a category."`
	Search CategorySearchCmd `cmd:"" help:"Search categories."`
}
type LocationCmd struct {
	Resolve LocationResolveCmd `cmd:"" help:"Resolve a place or postcode."`
}
type FilterCmd struct {
	List FilterListCmd `cmd:"" help:"List category filters."`
	Get  FilterGetCmd  `cmd:"" help:"Get one category filter."`
}
type ListingCmd struct {
	Get    ListingGetCmd    `cmd:"" help:"Get listing details."`
	Images ListingImagesCmd `cmd:"" help:"List or download listing images."`
	Open   ListingOpenCmd   `cmd:"" help:"Open the official listing URL."`
}
type SellerCmd struct {
	Get      SellerGetCmd      `cmd:"" help:"Get a known seller."`
	Search   SellerSearchCmd   `cmd:"" help:"Search locally encountered sellers."`
	Listings SellerListingsCmd `cmd:"" help:"List locally known seller listings."`
}
type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Log in through PKCE."`
	Status AuthStatusCmd `cmd:"" help:"Show local login status."`
	Logout AuthLogoutCmd `cmd:"" help:"Clear the local session."`
}
type DMCmd struct {
	List     DMListCmd     `cmd:"" help:"List conversations."`
	Get      DMGetCmd      `cmd:"" help:"Read a conversation."`
	MarkRead DMMarkReadCmd `cmd:"" name:"mark-read" help:"Mark conversations read."`
	Poll     DMPollCmd     `cmd:"" help:"Run one synchronization cycle."`
	Watch    DMWatchCmd    `cmd:"" help:"Watch DM changes through polling."`
	Reply    DMReplyCmd    `cmd:"" help:"Preview or confirm a reply."`
	Start    DMStartCmd    `cmd:"" help:"Preview or confirm a first contact."`
}
type SchemaCmd struct {
	List    SchemaListCmd    `cmd:"" help:"List operation schemas."`
	Show    SchemaShowCmd    `cmd:"" help:"Show a command schema."`
	Filters SchemaFiltersCmd `cmd:"" help:"Show the search schema with a category-filter overlay."`
}
type ConfigCmd struct {
	List ConfigListCmd `cmd:"" help:"List effective configuration."`
	Get  ConfigGetCmd  `cmd:"" help:"Get one effective value."`
	Set  ConfigSetCmd  `cmd:"" help:"Set one value atomically."`
	Path ConfigPathCmd `cmd:"" help:"Print the configuration path."`
}

type Runtime struct {
	Context   context.Context
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	RequestID string
	Profile   string
	Paths     platform.Paths
	Config    *app.ConfigStore
	Catalog   *schemacatalog.Catalog
	Core      *app.App
	Encoder   output.Encoder
	Quiet     bool
	Debug     bool
}

func (r *Runtime) Emit(schema string, data any) error {
	envelope := app.Envelope[any](r.Core.Clock, schema, r.RequestID, "local", data)
	return r.Encoder.Encode(r.Stdout, envelope)
}
func (r *Runtime) Diagnostic(format string, args ...any) {
	if !r.Quiet {
		_, _ = fmt.Fprintf(r.Stderr, format+"\n", args...)
	}
}

type exitPanic int

func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	root := &Root{}
	parser, err := kong.New(root, kong.Name("kcli"), kong.Description("Agent-friendly Kleinanzeigen CLI."), kong.Writers(stdout, stderr), kong.Exit(func(code int) { panic(exitPanic(code)) }), kong.Help(examplesHelpPrinter))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if exit, ok := recovered.(exitPanic); ok {
				code = int(exit)
				return
			}
			panic(recovered)
		}
	}()
	kongcompletion.Register(parser)
	catalog, err := app.BuildCatalog(parser.Model)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	runtime := &Runtime{Context: ctx, Stdin: stdin, Stdout: stdout, Stderr: stderr, RequestID: output.NewRequestID(), Catalog: schemacatalog.New(catalog, nil), Core: app.New(app.Dependencies{}), Encoder: output.Encoder{Format: defaultFormat(stdout)}}
	parsed, err := parser.Parse(args)
	if err != nil {
		return fail(runtime, invalidError(err), true)
	}
	runtime.Profile = root.Profile
	runtime.Quiet = root.Quiet
	runtime.Debug = root.Debug
	selected := selectedPath(parsed.Selected())
	if err := platform.ValidateProfile(root.Profile); err != nil {
		return fail(runtime, &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error()}, true)
	}
	if selected != "completion" {
		paths, err := platform.Resolve(root.Profile)
		if err != nil {
			return fail(runtime, &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error()}, true)
		}
		runtime.Paths = paths
		store, err := app.NewConfigStore(paths.ConfigFile)
		if err != nil {
			return fail(runtime, err, true)
		}
		runtime.Config = store
		effective, err := store.List(root.Profile)
		if err != nil {
			return fail(runtime, &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error(), Cause: err}, true)
		}
		if root.Output == "" && effective["output"].Value != "auto" {
			runtime.Encoder.Format, _ = output.ParseFormat(effective["output"].Value)
		}
		if !root.Quiet {
			runtime.Quiet, _ = strconv.ParseBool(effective["quiet"].Value)
		}
		if root.Timeout == 0 {
			root.Timeout, _ = time.ParseDuration(effective["timeout"].Value)
		}
	}
	if requiresRemoteState(selected) {
		database, err := state.Open(runtime.Context, runtime.Paths.StateDB)
		if err != nil {
			return fail(runtime, &domain.Error{Code: domain.CodeUnavailable, Message: "open profile state", Cause: err}, false)
		}
		defer database.Close()
		installID, err := database.MobileInstallID(runtime.Context, time.Now().UTC())
		if err != nil {
			return fail(runtime, &domain.Error{Code: domain.CodeUnavailable, Message: "load mobile install identity", Cause: err}, false)
		}
		credentials := kleinanzeigen.MobileCredentials{
			BasicUser:     os.Getenv("KLEINANZEIGEN_BASIC_USER"),
			BasicPassword: os.Getenv("KLEINANZEIGEN_BASIC_PW"),
			OAuthClientID: buildinfo.OAuthClientID,
		}
		mobile := kleinanzeigen.NewMobileTransport(kleinanzeigen.NewHTTPTransport(), installID, credentials, database)
		deps := app.Dependencies{Transport: mobile, State: database}
		if strings.HasPrefix(selected, "auth ") || strings.HasPrefix(selected, "dm ") {
			if store, err := secret.New("kcli"); err == nil {
				deps.Secrets = store
			}
		}
		runtime.Core = app.New(deps)
	}
	if root.Output != "" {
		format, err := output.ParseFormat(root.Output)
		if err != nil {
			return fail(runtime, &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error()}, true)
		}
		runtime.Encoder.Format = format
	}
	if root.Fields != "" {
		runtime.Encoder.Fields = strings.Split(root.Fields, ",")
		if selected != "" {
			if err := runtime.Catalog.ValidateFields(selected, runtime.Encoder.Fields); err != nil {
				return fail(runtime, invalidError(err), true)
			}
		}
	}
	if root.Timeout < 0 || root.Timeout > 60*time.Second {
		return fail(runtime, &domain.Error{Code: domain.CodeInvalidInput, Message: "timeout must be no greater than 60s"}, true)
	}
	if root.Timeout > 0 {
		deadlineContext, cancel := context.WithTimeout(runtime.Context, root.Timeout)
		defer cancel()
		runtime.Context = deadlineContext
	}
	if runtime.Debug {
		if meta, ok := catalog.Find(selected); ok {
			runtime.Diagnostic("debug: operation=%s request_id=%s output=%s schema=%s", selected, runtime.RequestID, runtime.Encoder.Format, meta.SchemaVersion)
		}
	}
	if err := parsed.Run(runtime); err != nil {
		return fail(runtime, err, false)
	}
	return 0
}
func selectedPath(node *kong.Node) string {
	var parts []string
	for current := node; current != nil && current.Type != kong.ApplicationNode; current = current.Parent {
		if current.Type == kong.CommandNode {
			parts = append(parts, current.Name)
		}
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, " ")
}

func requiresRemoteState(path string) bool {
	return strings.HasPrefix(path, "category ") ||
		strings.HasPrefix(path, "location ") ||
		strings.HasPrefix(path, "filter ") ||
		strings.HasPrefix(path, "search ") ||
		strings.HasPrefix(path, "listing ") ||
		strings.HasPrefix(path, "seller ") ||
		strings.HasPrefix(path, "auth ") ||
		strings.HasPrefix(path, "dm ")
}
func defaultFormat(stdout io.Writer) output.Format {
	if output.IsTTY(stdout) {
		return output.FormatTable
	}
	return output.FormatJSON
}
func invalidError(err error) error {
	return &domain.Error{Code: domain.CodeInvalidCommand, Message: err.Error(), Cause: err}
}
func fail(runtime *Runtime, err error, parse bool) int {
	if !runtime.Quiet {
		_, _ = fmt.Fprintln(runtime.Stderr, safeDiagnostic(err))
	}
	if runtime.Encoder.Format != output.FormatTable {
		_ = runtime.Encoder.EncodeError(runtime.Stdout, err, runtime.RequestID)
	}
	if parse {
		return 2
	}
	return domain.ExitCode(err)
}
func safeDiagnostic(err error) string {
	var typed *domain.Error
	if errors.As(err, &typed) {
		return typed.Message
	}
	return "operation failed"
}
