package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

type swappingMediaTransport struct{ duringDownload func() }

func (s swappingMediaTransport) AllowMediaURL(string) error { return nil }
func (s swappingMediaTransport) Do(kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	s.duringDownload()
	return kleinanzeigen.Response{StatusCode: 200, Headers: map[string][]string{"Content-Type": {"image/png"}}, Body: []byte("\x89PNG\r\n\x1a\nfixture")}, nil
}

func TestDownloadRejectsDirectoryReplacementDuringNetworkWait(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "overwrite"}[overwrite], func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			output, outside := filepath.Join(base, "downloads"), filepath.Join(base, "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			transport := swappingMediaTransport{duringDownload: func() {
				if err := os.Rename(output, output+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, output); err != nil {
					t.Fatal(err)
				}
			}}
			_, err = Download(context.Background(), transport, DownloadRequest{ListingID: "123", Relation: "large", URL: "https://img.kleinanzeigen.de/image.png", OutputDir: output, AllowOutsideCWD: output, Overwrite: overwrite})
			if err == nil {
				t.Error("replaced output directory accepted")
			}
			entries, readErr := os.ReadDir(outside)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("wrote outside authorized directory: %v err=%v", entries, readErr)
			}
		})
	}
}

func TestConcurrentDownloadsNeverClobber(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openDownloadDirectory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, body := range []string{"first", "second"} {
		go func(body string) {
			<-start
			results <- directory.writeAtomic(context.Background(), "image.png", []byte(body), false)
		}(body)
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful writers=%d", successes)
	}
	body, err := os.ReadFile(filepath.Join(base, "image.png"))
	if err != nil || (string(body) != "first" && string(body) != "second") {
		t.Fatalf("body=%q err=%v", body, err)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 {
		t.Fatalf("leftover files=%v err=%v", entries, err)
	}
}

func TestDownloadThroughSearchOnlyDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(base, "ancestor")
	output := filepath.Join(ancestor, "downloads")
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ancestor, 0700); _ = os.Chmod(output, 0700) })
	if err := os.Chmod(output, 0300); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, 0111); err != nil {
		t.Fatal(err)
	}
	directory, err := openDownloadDirectory(output)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := directory.writeAtomic(context.Background(), "image.png", []byte("fixture"), false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(output, "image.png"))
	if err != nil || string(body) != "fixture" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
