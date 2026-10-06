#!/usr/bin/env bash
# Browser tests of the web interface (web/e2e/, Playwright and axe) against a running
# Compose stack on the simulator's errand scenario, its user signed up and its Volvo ID
# connected: task compose-test runs them on its own stack; task e2e on task compose-sim.
#
#   scripts/e2e.sh <compose network>     # PLAYWRIGHT_IMAGE: the pinned Playwright image
#
# The browsers come from the Playwright image, in the stack's network: they reach
# http://web:8082. The test code and its runner come from web/node_modules, mounted read
# only: @playwright/test and @axe-core/playwright are plain JavaScript, so the host's
# install (macOS, arm64 or not) runs as is in the Linux container, at the versions of
# package-lock.json, with nothing to install. The runner refuses browsers of another
# version than its own: web/package.json pins @playwright/test to the image's version.
# Screenshots, and the report of a failed run, go to web/e2e-results/ (ignored by git).
set -euo pipefail
cd "$(dirname "$0")/.."

network=${1:?usage: scripts/e2e.sh <compose network>}
: "${PLAYWRIGHT_IMAGE:?set PLAYWRIGHT_IMAGE (the Taskfile does)}"
[ -x web/node_modules/.bin/playwright ] || { echo "e2e: run task web-install first" >&2; exit 1; }

# The mount point of the writable results, inside the read-only web/.
rm -rf web/e2e-results
mkdir -p web/e2e-results

# As the host user, so that the results stay the host's; HOME for the browsers' profile.
docker run --rm --init --ipc=host --network "$network" \
	--user "$(id -u):$(id -g)" -e HOME=/tmp -e CI="${CI:-}" \
	-e RUNSTEN_E2E_URL=http://web:8082 \
	-v "$PWD/web:/web:ro" -v "$PWD/web/e2e-results:/web/e2e-results" -w /web \
	"$PLAYWRIGHT_IMAGE" node_modules/.bin/playwright test
