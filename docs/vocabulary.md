# Vocabulary

Terms of docportal, one per line.

- docportal: the program built from cmd/docportal.
- API spec: an OpenAPI 3.0 or 3.1 document in YAML with a non-empty info.title, usually a bundle.
- bundle: an API spec whose $refs all point inside its own file, such as #/components/schemas/Pet.
- sample bundle: testdata/specs/petstore-3.0.yaml or testdata/specs/petstore-3.1.yaml, the bundles the tests render.
- operation: one method on one path of an API spec, such as GET /pets/{petId}.
- model: a schema under components/schemas, as Stoplight Elements lists and opens it.
- operator: the person who starts docportal and gives it its configuration.
- reader: the person who reads the documentation in a browser.
- arguments: docportal's command-line arguments, without the program name: the flags and nothing else.
- flags: -addr, -specs-dir and -spec-path. Any other flag is rejected.
- environment: a lookup of environment variables, of which docportal reads DOCPORTAL_ADDR, DOCPORTAL_SPECS_DIR and DOCPORTAL_SPEC_PATH. Tests pass their own lookup.
- configuration: the address, the directory name and the spec path, each taken from its flag, else from the environment, else from its default: :8080, . and openapi.yaml.
- startup: reading the configuration, opening the specs directory and building the portal, in main.startup. A failure there stops docportal before it listens.
- address: the network address docportal listens on, such as :8080.
- specs directory: the local directory that holds the API specs.
- directory name: the specs directory's path on the local file system, as the operator gives it.
- directory handle: the opened specs directory, an fs.FS. Every read of a spec goes through it, so no read leaves the directory.
- spec path: the path, relative to the specs directory, of the API spec to serve.
- configured spec: the file at the spec path. The portal serves no other file of the specs directory.
- missing spec: a spec path with no file behind it. It gets a 404 that names the file.
- invalid spec: a configured spec that is not an API spec: not YAML, not OpenAPI 3.0 or 3.1, or without a title. It gets an error page naming the file and the reason, and /api/specs/{spec path} answers with an error naming the file, both with status 422.
- error page: the HTML page the portal shows instead of the viewer page for an invalid or missing spec.
- portal configuration: the directory handle and the spec path, as portal.New takes them. Tests pass any fs.FS in the handle's place.
- portal: the HTTP handler that redirects / to the viewer page and serves the viewer page, the raw spec and the Elements assets.
- HTTP server: net/http's server, which listens on the address and calls the portal for each request.
- viewer page: the HTML page at /specs/{spec path} that embeds Stoplight Elements pointed at the raw spec.
- raw spec: the response at /api/specs/{spec path}: the configured spec's content, unchanged, as application/yaml.
- Stoplight Elements: the web component that renders an API spec as documentation in the reader's browser.
- Elements assets: the Stoplight Elements script, stylesheet and license, downloaded by `make setup` and embedded in docportal.
- opening routine: in the metaphor, startup.
- day's orders: in the metaphor, the configuration.
- stockroom: in the metaphor, the specs directory.
- counter: in the metaphor, the portal.
- doorman: in the metaphor, the HTTP server.
