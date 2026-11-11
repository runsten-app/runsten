# syntax=docker/dockerfile:1
# Image for a Runsten binary: static binary, distroless, non-root.
# Usage: docker build --build-arg CMD=runsten-simulator -t runsten-simulator .
#        docker build --build-arg CMD=runsten-web --target runsten-web -t runsten-web .
# The hosted offer's build adds its premium/ modules to the context and passes
# --build-arg TAGS=premium; without them, the public build.
# Base images are pinned by digest (multi-platform indexes); Dependabot keeps them up to date.
# The build stages run on the builder's platform and cross-compile for the target one
# (--platform linux/arm64 on an amd64 machine, for a Raspberry Pi): no emulation needed.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# A pattern, not a name: nothing to copy is no error.
COPY premiu[m] ./premium/
ARG CMD=runsten-simulator
ARG TAGS=""
# The release built, which the binaries tell (their main.version); dev otherwise.
ARG VERSION=dev
ARG TARGETOS TARGETARCH
# Go's build cache is the builder's, kept from one image to the next: the binaries share
# most of their packages, compiled once instead of once per image. It is keyed by what is
# compiled (sources, flags, tags, platform): nothing stale comes out of it.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -tags "${TAGS}" -o /out/app "./cmd/${CMD}"
# The directory runsten-api keeps the export archives in: a volume mounted there takes
# its owner, the image's non-root user (the runtime has no shell to create it).
RUN mkdir -p /out/exports

# The front end's build: HTML, JavaScript and CSS, the same for every platform. Its types
# are generated from the API description.
FROM --platform=$BUILDPLATFORM node:26.13.0-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc web/.nvmrc ./
RUN npm ci --no-audit
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web ./
RUN npm run build

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS runtime
# A release adds its version and commit (scripts/release-images.sh).
LABEL org.opencontainers.image.source="https://github.com/runsten-app/runsten" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"
COPY --from=build /out/app /app
COPY --from=build --chown=nonroot:nonroot /out/exports /var/lib/runsten/exports
USER nonroot:nonroot
ENTRYPOINT ["/app"]

# runsten-web serves the front end's build from /web.
FROM runtime AS runsten-web
COPY --from=web /src/web/dist /web
EXPOSE 8082

# Default target: the other binaries; the simulator reads its scenarios.
FROM runtime
COPY scenarios /scenarios
EXPOSE 8080
