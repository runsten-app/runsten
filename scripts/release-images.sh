#!/usr/bin/env bash
# Builds the images of a release for every platform and pushes them to the registry
# (ghcr.io/runsten-app by default), each with its SBOM and provenance, tagged with the
# version, its minor and latest; a pre-release (1.2.0-rc.1) only with its own tag, so
# that it moves no one's latest. Prints each image, by tag and digest, as Markdown for
# the release notes; the build's progress goes to stderr.
#
# Logged in to the registry first, with a buildx builder that pushes multi-platform
# images (docker buildx create --use: the docker-container driver). The Taskfile passes
# IMAGES and PLATFORMS (task release-images VERSION=…); the release workflow runs it.
#
#   IMAGES="runsten-api …" PLATFORMS="linux/amd64 linux/arm64" scripts/release-images.sh <version> [registry]
set -euo pipefail
cd "$(dirname "$0")/.."

version=${1:?usage: scripts/release-images.sh <version> [registry]}
registry=${2:-ghcr.io/runsten-app}
: "${IMAGES:?set IMAGES}" "${PLATFORMS:?set PLATFORMS}"
[[ $version =~ ^([0-9]+)\.([0-9]+)\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || {
	echo "not a version (X.Y.Z or X.Y.Z-pre): $version" >&2
	exit 2
}
minor=${BASH_REMATCH[1]}.${BASH_REMATCH[2]}
pre=${BASH_REMATCH[3]}
revision=$(git rev-parse HEAD)
source=https://github.com/runsten-app/runsten
meta=$(mktemp)
trap 'rm -f "$meta"' EXIT

declare -A description=(
	[runsten-api]="Runsten's API: sign-in, the Volvo ID connection, the JSON API"
	[runsten-collector]="Runsten's collector: polls the Volvo APIs, derives trips and charges"
	[runsten-web]="Runsten's web interface, relaying to runsten-api at the same origin"
	[runsten-simulator]="The Volvo API and Volvo ID simulator, for development"
)

for c in $IMAGES; do
	tags=(-t "$registry/$c:$version")
	[ -n "$pre" ] || tags+=(-t "$registry/$c:$minor" -t "$registry/$c:latest")
	target=()
	[ "$c" = runsten-web ] && target=(--target runsten-web)
	# The labels go into each platform's image, the annotations onto the index, which
	# the registry's page reads (the package's repository, licence and description).
	docker buildx build --push --platform "$(tr ' ' , <<<"$PLATFORMS")" \
		--build-arg CMD="$c" "${target[@]}" "${tags[@]}" \
		--label org.opencontainers.image.version="$version" \
		--label org.opencontainers.image.revision="$revision" \
		--annotation index:org.opencontainers.image.source="$source" \
		--annotation index:org.opencontainers.image.licenses=AGPL-3.0-or-later \
		--annotation index:org.opencontainers.image.version="$version" \
		--annotation index:org.opencontainers.image.revision="$revision" \
		--annotation index:org.opencontainers.image.description="${description[$c]:-$c}" \
		--sbom=true --provenance=mode=max \
		--metadata-file "$meta" --progress plain . >&2
	digest=$(jq -r '."containerimage.digest"' "$meta")
	[[ $digest == sha256:* ]] || {
		echo "$c: no digest in the build's metadata" >&2
		exit 1
	}
	echo "- \`$registry/$c:$version\` (\`$digest\`)"
done
