package platform

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

func OpenURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("URL must be an HTTPS Kleinanzeigen URL without user info or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host != "kleinanzeigen.de" && host != "www.kleinanzeigen.de" && host != "login.kleinanzeigen.de" {
		return fmt.Errorf("URL host is not allowed")
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{u.String()}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", u.String()}
	default:
		name, args = "xdg-open", []string{u.String()}
	}
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}
