package cli

import (
	"fmt"
	"runtime"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/buildinfo"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/platform"
	"github.com/johannhipp/kcli/internal/state"
)

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
		defer db.Close()
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
	if c.Network && db == nil {
		checks = append(checks, domain.DoctorCheckV1{Name: "network", Status: "skipped", Detail: "requires a healthy state database for request pacing"})
	} else if c.Network {
		service := kleinanzeigen.NewMetadataService(kleinanzeigen.NewWebTransport(db), db)
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
