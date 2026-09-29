# Run targets through devbox, e.g. `devbox run make test`.

ELEMENTS_VERSION := 9.0.25
ELEMENTS_SHA256  := 46ec1e31068195810725ae204950137a373ad0c2b78059ca07fd1580bfb79744
ELEMENTS_URL     := https://registry.npmjs.org/@stoplight/elements/-/elements-$(ELEMENTS_VERSION).tgz
ELEMENTS_TGZ     := bin/elements-$(ELEMENTS_VERSION).tgz
ELEMENTS_DIR     := portal/elements

# macOS has shasum but not always sha256sum.
SHA256SUM := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

# Claude Code cloud sessions provide Chromium here. Elsewhere this stays empty
# and chromedp looks for a locally installed Chrome.
CHROME_BIN ?= $(wildcard /opt/pw-browsers/chromium)
export CHROME_BIN

.PHONY: setup build lint test test-e2e run

setup: $(ELEMENTS_DIR)/web-components.min.js

$(ELEMENTS_DIR)/web-components.min.js:
	mkdir -p bin
	curl -fsSL -o $(ELEMENTS_TGZ) $(ELEMENTS_URL)
	echo "$(ELEMENTS_SHA256)  $(ELEMENTS_TGZ)" | $(SHA256SUM) -c -
	tar -xzf $(ELEMENTS_TGZ) -C $(ELEMENTS_DIR) --strip-components=1 package/web-components.min.js package/styles.min.css package/LICENSE
	rm $(ELEMENTS_TGZ)

build: setup
	go build -o bin/docportal ./cmd/docportal

lint:
	go vet ./...
	go vet -tags e2e ./e2e/...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

test: setup
	go test ./...

test-e2e: setup
	go test -tags e2e ./e2e/...

run: build
	./bin/docportal $(ARGS)
