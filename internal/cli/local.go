package cli

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/buildinfo"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/platform"
	"github.com/johannhipp/kcli/internal/state"
	kongcompletion "github.com/jotaen/kong-completion"
)

type SchemaListCmd struct{}

func (*SchemaListCmd) Run(runtime *Runtime) error {
	return runtime.Emit("kcli.schemas/v1", runtime.Catalog.List())
}
func (*SchemaListCmd) Describe() app.OperationMeta {
	return localOperation("List every command's operation metadata.", domain.SchemaListInputV1{}, domain.SchemaOutputV1{}, "kcli.schemas/v1", app.SideEffectNone, []string{"kcli schema list"})
}

type SchemaShowCmd struct {
	Command []string `arg:"" name:"command"`
}

func (c *SchemaShowCmd) Validate() error {
	if len(c.Command) == 0 || len(c.Command) > 3 {
		return fmt.Errorf("provide a command path")
	}
	for _, part := range c.Command {
		if err := validateReference(part, false); err != nil {
			return err
		}
	}
	return nil
}
func (c *SchemaShowCmd) Run(runtime *Runtime) error {
	document, err := runtime.Catalog.Show(strings.Join(c.Command, " "))
	if err != nil {
		return &domain.Error{Code: domain.CodeInvalidSchema, Message: err.Error(), Cause: err}
	}
	return runtime.Emit("kcli.schema/v1", document)
}
func (*SchemaShowCmd) Describe() app.OperationMeta {
	return localOperation("Show JSON input and output schemas plus operation annotations.", domain.SchemaShowInputV1{}, domain.SchemaOutputV1{}, "kcli.schema/v1", app.SideEffectNone, []string{"kcli schema show search"})
}

type SchemaFiltersCmd struct {
	Category string `required:""`
}

func (c *SchemaFiltersCmd) Validate() error { return validateReference(c.Category, true) }
func (c *SchemaFiltersCmd) Run(runtime *Runtime) error {
	result, err := runtime.Catalog.Filters(runtime.Context, c.Category)
	if err != nil {
		return &domain.Error{Code: domain.CodeInvalidSchema, Message: err.Error(), Cause: err}
	}
	return runtime.Emit("kcli.filter-schema/v1", map[string]any{"category": c.Category, "schema": result, "overlay": "cached"})
}
func (*SchemaFiltersCmd) Describe() app.OperationMeta {
	return localOperation("Overlay cached live filter metadata on the static search schema.", domain.SchemaFiltersInputV1{}, domain.SchemaOutputV1{}, "kcli.filter-schema/v1", app.SideEffectNone, []string{"kcli schema filters --category 278"})
}

type ConfigListCmd struct{}

func (*ConfigListCmd) Run(runtime *Runtime) error {
	values, err := runtime.Config.List(runtime.Profile)
	if err != nil {
		return localError(err)
	}
	return runtime.Emit("kcli.config/v1", values)
}
func (*ConfigListCmd) Describe() app.OperationMeta {
	return localOperation("List effective redacted configuration and each value's source.", domain.ConfigListInputV1{}, domain.ConfigOutputV1{}, "kcli.config/v1", app.SideEffectNone, []string{"kcli config list"})
}

type ConfigGetCmd struct {
	Key string `arg:""`
}

func (c *ConfigGetCmd) Validate() error { return validateReference(c.Key, false) }
func (c *ConfigGetCmd) Run(runtime *Runtime) error {
	value, err := runtime.Config.Get(runtime.Profile, c.Key)
	if err != nil {
		return localError(err)
	}
	return runtime.Emit("kcli.config-value/v1", map[string]any{"key": c.Key, "value": value.Value, "source": value.Source})
}
func (*ConfigGetCmd) Describe() app.OperationMeta {
	return localOperation("Get one effective configuration value and source.", domain.ConfigGetInputV1{}, domain.ConfigOutputV1{}, "kcli.config-value/v1", app.SideEffectNone, []string{"kcli config get output"})
}

type ConfigSetCmd struct {
	Key    string `arg:""`
	Value  string `arg:""`
	DryRun bool   `name:"dry-run"`
}

func (c *ConfigSetCmd) Validate() error {
	if err := validateReference(c.Key, false); err != nil {
		return err
	}
	if strings.IndexByte(c.Value, 0) >= 0 || len(c.Value) > 4096 {
		return fmt.Errorf("invalid config value")
	}
	return nil
}
func (c *ConfigSetCmd) Run(runtime *Runtime) error {
	change, err := runtime.Config.Set(runtime.Profile, c.Key, c.Value, c.DryRun)
	if err != nil {
		return localError(err)
	}
	return runtime.Emit("kcli.config-change/v1", change)
}
func (*ConfigSetCmd) Describe() app.OperationMeta {
	return localOperation("Validate and atomically set one profile or global configuration value.", domain.ConfigSetInputV1{}, domain.ConfigOutputV1{}, "kcli.config-change/v1", app.SideEffectLocal, []string{"kcli config set output json"})
}

type ConfigPathCmd struct{}

func (*ConfigPathCmd) Run(runtime *Runtime) error {
	return runtime.Emit("kcli.config-path/v1", map[string]string{"path": runtime.Config.Path()})
}
func (*ConfigPathCmd) Describe() app.OperationMeta {
	return localOperation("Show the platform configuration file path.", domain.ConfigPathInputV1{}, domain.ConfigOutputV1{}, "kcli.config-path/v1", app.SideEffectNone, []string{"kcli config path"})
}

type DoctorCmd struct {
	Network bool
}

func (c *DoctorCmd) Run(runtimeContext *Runtime) error {
	checks := []domain.DoctorCheckV1{
		{Name: "build", Status: "ok", Detail: buildinfo.Current().Version + " " + runtime.GOOS + "/" + runtime.GOARCH},
		{Name: "config", Status: "ok", Detail: runtimeContext.Paths.ConfigFile},
	}
	if _, err := runtimeContext.Config.Load(); err != nil {
		checks[1] = domain.DoctorCheckV1{Name: "config", Status: "error", Detail: err.Error()}
	}
	db, err := state.Open(runtimeContext.Context, runtimeContext.Paths.StateDB)
	if err != nil {
		checks = append(checks, domain.DoctorCheckV1{Name: "state", Status: "error", Detail: "state database could not be opened"})
	} else {
		tables, tableErr := db.SchemaTables(runtimeContext.Context)
		_ = db.Close()
		if tableErr != nil {
			checks = append(checks, domain.DoctorCheckV1{Name: "state", Status: "error", Detail: "schema health check failed"})
		} else {
			checks = append(checks, domain.DoctorCheckV1{Name: "state", Status: "ok", Detail: fmt.Sprintf("%d schema tables; synchronous=FULL; 5s busy timeout", len(tables))})
		}
	}
	if free, freeErr := platform.FreeBytes(runtimeContext.Paths.StateDB); freeErr != nil {
		checks = append(checks, domain.DoctorCheckV1{Name: "disk_space", Status: "error", Detail: "free space could not be measured"})
	} else if free < 100<<20 {
		checks = append(checks, domain.DoctorCheckV1{Name: "disk_space", Status: "warning", Detail: fmt.Sprintf("%d bytes available", free)})
	} else {
		checks = append(checks, domain.DoctorCheckV1{Name: "disk_space", Status: "ok", Detail: fmt.Sprintf("%d bytes available", free)})
	}
	checks = append(checks, domain.DoctorCheckV1{Name: "backend", Status: "ok", Detail: "public website; no login or application credentials required"})
	if c.Network {
		service, serviceErr := metadataService(runtimeContext)
		if serviceErr != nil {
			return serviceErr
		}
		_, probeErr := service.Categories(runtimeContext.Context, true)
		if probeErr != nil {
			return probeErr
		}
		checks = append(checks, domain.DoctorCheckV1{Name: "network", Status: "ok", Detail: "public category response parsed successfully"})
	} else {
		checks = append(checks, domain.DoctorCheckV1{Name: "network", Status: "skipped", Detail: "local-only by default"})
	}
	return runtimeContext.Emit("kcli.doctor/v1", checks)
}
func (*DoctorCmd) Describe() app.OperationMeta {
	return localOperation("Run local diagnostics; --network explicitly checks public category access.", domain.DoctorInputV1{}, domain.DoctorOutputV1{}, "kcli.doctor/v1", app.SideEffectLocal, []string{"kcli doctor"})
}

type CompletionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish"`
}

func (c *CompletionCmd) Run(ctx *kong.Context, _ *Runtime) error {
	completion := kongcompletion.Completion{Shell: c.Shell, Code: true}
	return completion.Run(ctx)
}
func (*CompletionCmd) Describe() app.OperationMeta {
	return localOperation("Print shell initialization code derived from the Kong model.", domain.CompletionInputV1{}, domain.CompletionOutputV1{}, "kcli.completion/v1", app.SideEffectNone, []string{"kcli completion bash"})
}

type VersionCmd struct{}

func (*VersionCmd) Run(runtime *Runtime) error {
	return runtime.Emit("kcli.version/v1", buildinfo.Current())
}
func (*VersionCmd) Describe() app.OperationMeta {
	return localOperation("Print version, build, contract, and transport metadata.", domain.VersionInputV1{}, domain.VersionOutputV1{}, "kcli.version/v1", app.SideEffectNone, []string{"kcli version"})
}

func localOperation(purpose string, input, output any, schema string, side app.SideEffect, examples []string) app.OperationMeta {
	inputType, outputType := reflect.TypeOf(input), reflect.TypeOf(output)
	return app.OperationMeta{Purpose: purpose, InputName: inputType.Name(), OutputName: outputType.Name(), SchemaVersion: schema, SideEffect: side, Evidence: app.EvidenceLocal, Examples: examples, StoryIDs: []string{}, InputType: inputType, OutputType: outputType}
}
func localError(err error) error {
	return &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error(), Cause: err}
}
