# syntax=docker/dockerfile:1

# The example application's image. Build it from the repository's root, since
# the example's module builds against the library of the checkout:
#
#   docker build -f examples/portalapp/Dockerfile --secret id=netrc,src=$HOME/.netrc -t portalapp .
#
# The library needs the private module github.com/bilus/live-templ, which Go
# fetches with git: the secret netrc holds a GitHub token that reads it, and
# no layer keeps it. The build arguments GOPROXY and GOPRIVATE suit a module
# proxy of one's own.
FROM golang:1.27 AS build
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOPRIVATE=github.com/bilus/live-templ
WORKDIR /src
COPY . .
# make setup downloads the Elements assets, which the portal package embeds.
RUN --mount=type=secret,id=netrc,target=/root/.netrc,required=false \
    --mount=type=cache,target=/go/pkg/mod \
    make setup && cd examples/portalapp && CGO_ENABLED=0 go build -trimpath -o /out/portalapp .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/portalapp /portalapp
EXPOSE 8080
ENTRYPOINT ["/portalapp"]
