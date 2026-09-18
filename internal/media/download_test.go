package media

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

type listingMediaTransport struct {
	responses map[string]kleinanzeigen.Response
	requests  []kleinanzeigen.Request
	allowed   []string
}

func (t *listingMediaTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	t.requests = append(t.requests, request)
	response, ok := t.responses[request.AbsoluteURL]
	if !ok {
		return kleinanzeigen.Response{}, errors.New("unexpected media URL")
	}
	return response, nil
}

func (t *listingMediaTransport) AllowMediaURL(raw string) error {
	t.allowed = append(t.allowed, raw)
	return nil
}

func TestDownloadValidatesRedirectAndWritesAtomically(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	root, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	first := "https://img.kleinanzeigen.de/image/first"
	second := "https://cdn.kleinanzeigen.de/image/final"
	transport := &listingMediaTransport{responses: map[string]kleinanzeigen.Response{
		first:  {StatusCode: http.StatusFound, Headers: map[string][]string{"Location": {second}}},
		second: {StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"image/png"}}, Body: []byte("\x89PNG\r\n\x1a\nfixture")},
	}}
	path, err := Download(context.Background(), transport, DownloadRequest{ListingID: "123", Index: 0, Relation: "XXL", URL: first, OutputDir: "downloads", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "downloads", "123-0-XXL.png") || len(transport.requests) != 2 || len(transport.allowed) != 2 {
		t.Fatalf("path=%q requests=%d allowed=%v", path, len(transport.requests), transport.allowed)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "\x89PNG\r\n\x1a\nfixture" {
		t.Fatalf("downloaded body=%q err=%v", body, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v err=%v", entries, err)
	}
}

func TestDownloadRejectsPrivateRedirectSizeAndMIME(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	tests := []struct {
		name     string
		response kleinanzeigen.Response
		max      int64
		wantCode domain.ErrorCode
	}{
		{name: "private redirect", response: kleinanzeigen.Response{StatusCode: http.StatusFound, Headers: map[string][]string{"Location": {"https://127.0.0.1/private"}}}, max: 100, wantCode: domain.CodeUpstreamContract},
		{name: "too large", response: kleinanzeigen.Response{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"image/png"}}, Body: png}, max: 4, wantCode: domain.CodeUnavailable},
		{name: "MIME mismatch", response: kleinanzeigen.Response{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"image/jpeg"}}, Body: png}, max: 100, wantCode: domain.CodeUpstreamContract},
		{name: "magic mismatch", response: kleinanzeigen.Response{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"image/png"}}, Body: []byte("not an image")}, max: 100, wantCode: domain.CodeUpstreamContract},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &listingMediaTransport{responses: map[string]kleinanzeigen.Response{"https://img.kleinanzeigen.de/start": test.response}}
			canonical, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			outputDir := filepath.Join(canonical, "out")
			_, err = Download(context.Background(), transport, DownloadRequest{ListingID: "123", Index: 0, Relation: "large", URL: "https://img.kleinanzeigen.de/start", OutputDir: outputDir, AllowOutsideCWD: outputDir, MaxBytes: test.max})
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != test.wantCode {
				t.Fatalf("error=%#v want code %s", err, test.wantCode)
			}
		})
	}
}

func TestDownloadPathSandboxSymlinkCollisionAndOverwrite(t *testing.T) {
	root := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	root, _ = os.Getwd()
	defer os.Chdir(previous)
	transport := &listingMediaTransport{responses: map[string]kleinanzeigen.Response{"https://img.kleinanzeigen.de/image": {StatusCode: 200, Headers: map[string][]string{"Content-Type": {"image/jpeg"}}, Body: []byte{0xff, 0xd8, 0xff, 0x00}}}}
	base := DownloadRequest{ListingID: "123", Index: 2, Relation: "large", URL: "https://img.kleinanzeigen.de/image", OutputDir: "out", MaxBytes: 100}
	path, err := Download(context.Background(), transport, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Download(context.Background(), transport, base); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("collision was not rejected: %v", err)
	}
	base.Overwrite = true
	if replaced, err := Download(context.Background(), transport, base); err != nil || replaced != path {
		t.Fatalf("overwrite path=%q err=%v", replaced, err)
	}
	outside := filepath.Join(root, "..", "outside")
	base.OutputDir, base.AllowOutsideCWD, base.Overwrite = outside, "", false
	if _, err := Download(context.Background(), transport, base); err == nil {
		t.Fatal("outside path accepted without exact opt-in")
	}
	base.AllowOutsideCWD = outside
	if _, err := Download(context.Background(), transport, base); err != nil {
		t.Fatalf("exact outside opt-in rejected: %v", err)
	}
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	base.OutputDir, base.AllowOutsideCWD = "linked/images", ""
	if _, err := Download(context.Background(), transport, base); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink parent accepted: %v", err)
	}
}

func TestDownloadInterruptionLeavesNoOwnedTemporaryFile(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openDownloadDirectory(canonical)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	err = directory.writeAtomic(ctx, "image.png", []byte("\x89PNG\r\n\x1a\n"), false)
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeInterrupted {
		t.Fatalf("error=%#v", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("interrupted write left files: %v err=%v", entries, readErr)
	}
}
