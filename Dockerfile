# syntax=docker/dockerfile:1
# Image for a Runsten binary: static binary, distroless, non-root.
# Usage: docker build --build-arg CMD=runsten-simulator -t runsten-simulator .
#        docker build --build-arg CMD=runsten-web --target runsten-web -t runsten-web .
# The hosted offer's build adds its premium/ modules to the context and passes
# --build-arg TAGS=premium; without them, the public build.
# Base images are pinned by digest (multi-platform indexes); Dependabot keeps them up to date.
# The build stages run on the builder's platform and cross-compile for the target one
# (--platform linux/arm64 on an amd64 machine, for a Raspberry Pi): no emulation needed.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# A pattern, not a name: nothing to copy is no error.
COPY premiu[m] ./premium/
ARG CMD=runsten-simulator
ARG TAGS=""
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -tags "${TAGS}" -o /out/app "./cmd/${CMD}"

# The front end's build: HTML, JavaScript and CSS, the same for every platform. Its types
# are generated from the API description.
FROM --platform=$BUILDPLATFORM node:26.12.1-alpine@sha256:40cd011b55415bc894e45b02fdd5a5dfde056921b7d23a64c74f80497af48797 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc web/.nvmrc ./
RUN npm ci --no-audit
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web ./
RUN npm run build

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99 AS runtime
# A release adds its version and commit (scripts/release-images.sh).
LABEL org.opencontainers.image.source="https://github.com/runsten-app/runsten" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"
COPY --from=build /out/app /app
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
