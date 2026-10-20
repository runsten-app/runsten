# syntax=docker/dockerfile:1
# Image for a Runsten binary: static binary, distroless, non-root.
# Usage: docker build --build-arg CMD=runsten-simulator -t runsten-simulator .
#        docker build --build-arg CMD=runsten-web --target runsten-web -t runsten-web .
# The hosted offer's build adds its premium/ modules to the context and passes
# --build-arg TAGS=premium; without them, the public build.
# Base images are pinned by digest (multi-platform indexes); Dependabot keeps them up to date.
# The build stages run on the builder's platform and cross-compile for the target one
# (--platform linux/arm64 on an amd64 machine, for a Raspberry Pi): no emulation needed.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:738d1cf061836894ff6bb8c33881080ac66de8cf0586615012a0c8f592649cfa AS build
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
FROM --platform=$BUILDPLATFORM node:26.12.0-alpine@sha256:6f08a523443ddedf52941874c203475b0ad503e859ad70cb26ab8be9a517dbd5 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc web/.nvmrc ./
RUN npm ci --no-audit
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web ./
RUN npm run build

FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087 AS runtime
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
