# kcli — common developer commands.
# The full pre-merge gate is `make check`.

GO ?= go
GOTESTFLAGS := -tags testing -race -shuffle=on

.PHONY: build test vet lint fmt check-docs check cross clean

build:
	$(GO) build ./...

test:
	$(GO) test $(GOTESTFLAGS) ./...

vet:
	$(GO) vet ./...

lint:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

fmt:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

check-docs:
	python3 scripts/check_docs.py

check: fmt vet test lint check-docs

cross:
	for os in darwin linux windows; do \
		for arch in amd64 arm64; do \
			GOOS=$$os GOARCH=$$arch $(GO) build -o /tmp/kcli-$$os-$$arch ./cmd/kcli || exit 1; \
		done; \
	done

clean:
	rm -f /tmp/kcli-* kcli
