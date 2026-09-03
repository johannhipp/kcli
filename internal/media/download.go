package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

const listingDefaultMaxBytes int64 = 25 << 20

var listingFilenamePartPattern = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type DownloadRequest struct {
	ListingID       string
	Index           int
	Relation        string
	URL             string
	OutputDir       string
	AllowOutsideCWD string
	MaxBytes        int64
	Overwrite       bool
}

func Download(ctx context.Context, transport kleinanzeigen.Transport, request DownloadRequest) (string, error) {
	if transport == nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "media transport is unavailable"}
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = listingDefaultMaxBytes
	}
	if request.MaxBytes < 1 || request.MaxBytes > listingDefaultMaxBytes || request.Index < 0 || request.ListingID == "" || request.Relation == "" {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid media download request"}
	}
	if err := listingValidateMediaURL(request.URL); err != nil {
		return "", err
	}
	outputDir, err := listingResolveOutputDir(request.OutputDir, request.AllowOutsideCWD)
	if err != nil {
		return "", err
	}
	if err := listingPrepareDirectory(outputDir); err != nil {
		var typed *domain.Error
		if errors.As(err, &typed) {
			return "", err
		}
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "prepare image output directory", Cause: err}
	}
	relation := strings.Trim(listingFilenamePartPattern.ReplaceAllString(request.Relation, "_"), "_")
	if relation == "" {
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "image relation cannot form a safe filename"}
	}
	body, contentType, err := listingFetchMedia(ctx, transport, request.URL, request.MaxBytes)
	if err != nil {
		return "", err
	}
	extension, err := listingImageExtension(contentType, body)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d-%s.%s", request.ListingID, request.Index, relation, extension)
	destination := filepath.Join(outputDir, name)
	if filepath.Dir(destination) != outputDir {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid image destination"}
	}
	if err := listingWriteAtomic(ctx, destination, body, request.Overwrite); err != nil {
		return "", err
	}
	return destination, nil
}

func listingFetchMedia(ctx context.Context, transport kleinanzeigen.Transport, raw string, maxBytes int64) ([]byte, string, error) {
	current := raw
	for redirects := 0; redirects <= 5; redirects++ {
		if err := listingValidateMediaURL(current); err != nil {
			return nil, "", err
		}
		if err := kleinanzeigen.ListingAllowMediaURL(transport, current); err != nil {
			return nil, "", &domain.Error{Code: domain.CodeUnavailable, Message: "media URL policy is unavailable", Cause: err}
		}
		response, err := listingDoMedia(transport, kleinanzeigen.Request{Context: ctx, Host: kleinanzeigen.HostMedia, Method: http.MethodGet, AbsoluteURL: current, MaxResponseBytes: maxBytes + 1, Class: kleinanzeigen.VolatileRead}, maxBytes)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location := listingHeader(response.Headers, "Location")
			if location == "" {
				return nil, "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "media redirect omitted Location"}
			}
			base, _ := url.Parse(current)
			next, err := base.Parse(location)
			if err != nil {
				return nil, "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "media redirect URL is invalid"}
			}
			current = next.String()
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, "", kleinanzeigen.ResponseError(response)
		}
		if int64(len(response.Body)) > maxBytes {
			return nil, "", &domain.Error{Code: domain.CodeUnavailable, Message: fmt.Sprintf("image exceeds %d byte limit", maxBytes)}
		}
		return response.Body, listingHeader(response.Headers, "Content-Type"), nil
	}
	return nil, "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "media redirect limit exceeded"}
}

func listingDoMedia(transport kleinanzeigen.Transport, request kleinanzeigen.Request, maxBytes int64) (kleinanzeigen.Response, error) {
	opener, ok := transport.(interface {
		OpenMedia(kleinanzeigen.Request) (kleinanzeigen.MediaResponse, error)
	})
	if !ok {
		return transport.Do(request)
	}
	response, err := opener.OpenMedia(request)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return kleinanzeigen.Response{}, &domain.Error{Code: domain.CodeConnectivity, Message: "read media response", Cause: err}
	}
	return kleinanzeigen.Response{StatusCode: response.StatusCode, Headers: response.Headers, Body: body}, nil
}

func listingValidateMediaURL(raw string) error {
	if raw == "" || len(raw) > 8192 || strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "returned media URL is invalid"}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" {
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "returned media URL must be an exact HTTPS URL"}
	}
	decodedPath, pathErr := url.PathUnescape(parsed.EscapedPath())
	decodedQuery, queryErr := url.QueryUnescape(parsed.RawQuery)
	if pathErr != nil || queryErr != nil || strings.IndexFunc(decodedPath, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 || strings.IndexFunc(decodedQuery, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "returned media URL contains encoded control characters"}
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(host) != nil || (host != "kleinanzeigen.de" && !strings.HasSuffix(host, ".kleinanzeigen.de")) {
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "returned media URL is outside the Kleinanzeigen CDN policy"}
	}
	return nil
}

func listingImageExtension(contentType string, body []byte) (string, error) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	magicType, extension := "", ""
	switch {
	case len(body) >= 3 && body[0] == 0xff && body[1] == 0xd8 && body[2] == 0xff:
		magicType, extension = "image/jpeg", "jpg"
	case len(body) >= 8 && string(body[:8]) == "\x89PNG\r\n\x1a\n":
		magicType, extension = "image/png", "png"
	case len(body) >= 6 && (string(body[:6]) == "GIF87a" || string(body[:6]) == "GIF89a"):
		magicType, extension = "image/gif", "gif"
	case len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP":
		magicType, extension = "image/webp", "webp"
	case len(body) >= 12 && string(body[4:8]) == "ftyp" && (string(body[8:12]) == "avif" || string(body[8:12]) == "avis"):
		magicType, extension = "image/avif", "avif"
	default:
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "downloaded media did not have a supported image magic prefix"}
	}
	if mediaType == "image/jpg" {
		mediaType = "image/jpeg"
	}
	if mediaType != magicType {
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "downloaded media content type did not match its image bytes", Details: map[string]any{"content_type": mediaType, "detected_type": magicType}}
	}
	return extension, nil
}

func listingResolveOutputDir(input, allowed string) (string, error) {
	if input == "" {
		input = "./kcli-downloads"
	}
	if strings.IndexByte(input, 0) >= 0 || filepath.Clean(input) == "." {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid image output directory"}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "resolve current working directory", Cause: err}
	}
	absolute := input
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(cwd, input)
	}
	absolute = filepath.Clean(absolute)
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "resolve image output directory", Cause: err}
	}
	escapes := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
	if (filepath.IsAbs(input) || escapes) && allowed != input {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "absolute or current-directory-escaping output requires the exact same path through --allow-outside-cwd"}
	}
	if allowed != "" && allowed != input {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "--allow-outside-cwd must exactly match --output-dir"}
	}
	return absolute, nil
}

func listingPrepareDirectory(path string) error {
	if err := listingRejectSymlinkParents(path); err != nil {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error(), Cause: err}
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := listingRejectSymlinkParents(path); err != nil {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: err.Error(), Cause: err}
	}
	return os.Chmod(path, 0o700)
}

func listingRejectSymlinkParents(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(remainder, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component is not allowed: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("output path component is not a directory: %s", current)
		}
	}
	return nil
}

func listingWriteAtomic(ctx context.Context, destination string, body []byte, overwrite bool) (err error) {
	if err := ctx.Err(); err != nil {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "image download interrupted", Cause: err}
	}
	if info, statErr := os.Lstat(destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination is not a regular file"}
		}
		if !overwrite {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination already exists; use --overwrite to replace it"}
		}
	} else if !os.IsNotExist(statErr) {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "inspect image destination", Cause: statErr}
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "create image temporary name", Cause: err}
	}
	temporary := destination + ".tmp-" + hex.EncodeToString(suffix)
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "create image temporary file", Cause: err}
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(body); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "write image temporary file", Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "image download interrupted", Cause: err}
	}
	if err := file.Sync(); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "sync image temporary file", Cause: err}
	}
	if err := file.Close(); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "close image temporary file", Cause: err}
	}
	if !overwrite {
		reservation, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination already exists; use --overwrite to replace it", Cause: err}
		}
		if err := reservation.Close(); err != nil {
			_ = os.Remove(destination)
			return &domain.Error{Code: domain.CodeUnavailable, Message: "reserve image destination", Cause: err}
		}
		if err := os.Rename(temporary, destination); err != nil {
			_ = os.Remove(destination)
			return &domain.Error{Code: domain.CodeUnavailable, Message: "install downloaded image", Cause: err}
		}
	} else if err := os.Rename(temporary, destination); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "replace downloaded image", Cause: err}
	}
	complete = true
	return nil
}

func listingHeader(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
