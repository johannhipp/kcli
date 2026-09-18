package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

type brokenOutputWriter struct{}

func (brokenOutputWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTableOutputFailureReturnsNonzero(t *testing.T) {
	setPathEnvironment(t)
	var stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"--output", "table", "version"}, strings.NewReader(""), brokenOutputWriter{}, &stderr); code == 0 {
		t.Fatal("failed table output reported success")
	}
}
