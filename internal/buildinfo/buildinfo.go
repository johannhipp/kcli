package buildinfo

import "runtime"

var (
	Version          = "dev"
	Commit           = "unknown"
	Date             = "unknown"
	AppVersion       = "unknown"
	APIContract      = "2026-09-18"
	TransportProfile = "public-web"
	BasicUser        string
	BasicPassword    string
	OAuthClientID    string
)

type Info struct {
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	Date             string `json:"date"`
	GoVersion        string `json:"go_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	AppVersion       string `json:"app_version"`
	APIContract      string `json:"api_contract"`
	TransportProfile string `json:"transport_profile"`
}

func Current() Info {
	return Info{
		Version: Version, Commit: Commit, Date: Date, GoVersion: runtime.Version(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, AppVersion: AppVersion,
		APIContract: APIContract, TransportProfile: TransportProfile,
	}
}

func DistributionConfigured() bool { return BasicUser != "" && BasicPassword != "" }
func OAuthConfigured() bool        { return OAuthClientID != "" }
