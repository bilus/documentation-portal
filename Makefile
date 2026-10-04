# Run targets through devbox, e.g. `devbox run make test`.

ELEMENTS_VERSION := 9.0.25
ELEMENTS_SHA256  := 46ec1e31068195810725ae204950137a373ad0c2b78059ca07fd1580bfb79744
ELEMENTS_URL     := https://registry.npmjs.org/@stoplight/elements/-/elements-$(ELEMENTS_VERSION).tgz
ELEMENTS_TGZ     := bin/elements-$(ELEMENTS_VERSION).tgz
ELEMENTS_DIR     := portal/elements
ELEMENTS_ASSETS  := web-components.min.js styles.min.css LICENSE
ELEMENTS_FILES   := $(addprefix $(ELEMENTS_DIR)/,$(ELEMENTS_ASSETS))

# macOS has shasum but not always sha256sum.
SHA256SUM := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

# Claude Code cloud sessions provide Chromium here. Elsewhere this stays empty
# and chromedp looks for a locally installed Chrome.
CHROME_BIN ?= $(wildcard /opt/pw-browsers/chromium)
export CHROME_BIN

DEPGRAPH ?= go run github.com/bilus/Scratchpad/go-depgraph/cmd/depgraph@latest
DIAGRAM   = docs/diagrams/packages.svg
LAYERS    = $(wildcard docs/diagrams/layers.toml)

.PHONY: setup generate build lint test test-e2e run diagram

# The core packages leave sign-in to the program that embeds them: none may
# import the sign-in package, an OAuth or OpenID Connect library or an
# identity provider's package. docportal is such a program, so the check
# leaves it out: its GCS and S3 drivers need OAuth for the bucket's
# credentials, and its sign-in needs the sign-in package.
CORE_PACKAGES := ./portal ./source ./chat ./anthropicmodel
SIGN_IN       := ^(github\.com/bilus/documentation-portal/signin|golang\.org/x/oauth2|github\.com/coreos/go-oidc|github\.com/zitadel/oidc|github\.com/markbates/goth|github\.com/auth0|github\.com/okta)(/|$$)

setup: $(ELEMENTS_FILES)

# &: makes one run of the recipe produce all three assets. The Makefile
# prerequisite reruns the download after a version bump. tar -m gives the
# assets the current time, because npm dates every file in its tarballs
# 1985-10-26, which would leave them older than the Makefile.
$(ELEMENTS_FILES) &: Makefile
	mkdir -p bin
	curl -fsSL -o $(ELEMENTS_TGZ) $(ELEMENTS_URL)
	echo "$(ELEMENTS_SHA256)  $(ELEMENTS_TGZ)" | $(SHA256SUM) -c -
	tar -m -xzf $(ELEMENTS_TGZ) -C $(ELEMENTS_DIR) --strip-components=1 $(addprefix package/,$(ELEMENTS_ASSETS))
	rm $(ELEMENTS_TGZ)

# livegen compiles chat/page.templ into page_templ.go and page_live.go, which
# are committed; run it after changing the template.
generate:
	go tool livegen chat
	gofmt -w chat/page_templ.go chat/page_live.go

build: setup
	go build -o bin/docportal ./cmd/docportal

lint:
	go vet ./...
	go vet -tags e2e ./e2e/...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	@deps=$$(go list -deps $(CORE_PACKAGES)) || exit 1; \
	found=$$(echo "$$deps" | grep -E '$(SIGN_IN)'); \
	test -z "$$found" || { echo "sign-in packages in the core packages:"; echo "$$found"; exit 1; }

test: setup
	go test ./...

test-e2e: setup
	go test -tags e2e ./e2e/...

run: build
	./bin/docportal $(ARGS)

# diagram draws the packages and their imports, with the layers that
# docs/diagrams/layers.toml asserts, and opens the drawing.
diagram:
	@mkdir -p $(dir $(DIAGRAM))
	$(DEPGRAPH) \
		-exclude 'e2e/...' \
		-exclude 'internal/...' \
		$(if $(LAYERS),-layers $(LAYERS),) -o $(DIAGRAM) .
	open $(DIAGRAM)
