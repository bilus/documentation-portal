# Run targets through devbox, e.g. `devbox run make test`.

# Claude Code cloud sessions provide Chromium here. Elsewhere this stays empty
# and chromedp looks for a locally installed Chrome.
CHROME_BIN ?= $(wildcard /opt/pw-browsers/chromium)
export CHROME_BIN

.PHONY: build lint test test-e2e run

build:
	go build -o bin/docportal ./cmd/docportal

lint:
	go vet ./...
	go vet -tags e2e ./e2e/...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

test:
	go test ./...

test-e2e:
	go test -tags e2e ./e2e/...

run: build
	./bin/docportal $(ARGS)
