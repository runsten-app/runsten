#!/usr/bin/env bash
# Updates a self-hosted Runsten to a release (documentation/self-hosting/updating.md),
# from the directory of the installation, where it is fetched with the release's files:
#
#   deploy/update.sh             # to the latest release
#   deploy/update.sh 0.11.0      # to that one
#   deploy/update.sh --check     # only says whether a newer release is out
#   deploy/update.sh --restore backups/runsten-0.10.1-….dump 0.10.1   # the way back
#
# It finds the release, runs that release's own update.sh when it is newer and differs
# from this one, stops when the release's .env.example has a variable .env lacks, shows the
# update notes since the release running, asks, pulls the images, backs the database
# up into backups/, installs the files, sets RUNSTEN_VERSION in .env and restarts;
# then, when a note asks for it, rebuilds the trips and charges. Nothing is changed
# before the question, nor when the images cannot be pulled.
#
# Needs bash, curl and Docker Compose. Reads GitHub: the API for the latest release,
# raw.githubusercontent.com for the files.
set -euo pipefail

repo=runsten-app/runsten
raw=https://raw.githubusercontent.com/$repo
releases=https://github.com/$repo/releases
# The files of an installation, as documentation/self-hosting/index.md fetches them.
files=(compose.yaml .env.example deploy/postgres/init-runsten.sh deploy/mosquitto/mosquitto.conf)
semver='^[0-9]+\.[0-9]+\.[0-9]+$'

usage() {
	cat <<-EOF
		usage: deploy/update.sh [--check] [--yes] [--no-backup] [--restore DUMP] [X.Y.Z]

		Updates Runsten to the release X.Y.Z, the latest one without it.
		  --check         only say whether a newer release is out
		  --yes           ask nothing: update, and rebuild when the notes ask for it
		  --no-backup     take no backup of the database first
		  --restore DUMP  replace the database with DUMP, a backup taken before the
		                  update to X.Y.Z: the way back
	EOF
}

fail() {
	echo "update: $*" >&2
	exit 1
}

# newer A B: whether the version A comes after B.
newer() {
	local -a a b
	local i
	IFS=. read -r -a a <<<"$1"
	IFS=. read -r -a b <<<"$2"
	for i in 0 1 2; do
		((10#${a[i]} > 10#${b[i]})) && return 0
		((10#${a[i]} < 10#${b[i]})) && return 1
	done
	return 1
}

confirm() {
	$yes && return 0
	[ -r /dev/tty ] || fail "nothing done: no terminal to ask on (--yes to go on without asking)"
	local answer
	read -r -p "$1 [y/N] " answer </dev/tty
	[[ $answer == [yY]* ]]
}

# The release running: the version label of api's image, as a release builds it; else
# the one .env pins.
running() {
	local id version=
	id=$(docker compose ps -q api 2>/dev/null) || true
	[ -z "$id" ] || version=$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$id" 2>/dev/null) || true
	[[ $version =~ $semver ]] || version=$(sed -n "s/^RUNSTEN_VERSION=[\"']*\([^\"']*\).*/\1/p" .env | tail -n 1)
	[[ $version =~ $semver ]] && echo "$version" || true
}

latest() {
	curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
		sed -n 's/^ *"tag_name": *"v\([^"]*\)".*/\1/p' | head -n 1
}

# The update notes of a release: the bullets of its updating.md's section, one a line.
# Fails when updating.md cannot be read: every release has one.
notes() {
	local md
	md=$(curl -fsSL "$raw/v$1/documentation/self-hosting/updating.md") || return 1
	sed -n '/^## Update notes/,/^## [^U]/p' <<<"$md" | grep '^- ' || true
}

# The variables a .env-like file sets, one a line, sorted.
variables() { sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$1" | sort -u; }

# explain FILE NAMES: each variable of NAMES (one a line) with the comments above it in
# FILE.
explain() {
	awk -v list="$(tr '\n' ' ' <<<"$2")" '
		BEGIN { split(list, n, " "); for (i in n) names[n[i]] = 1 }
		/^#/ { block = block $0 "\n"; next }
		/^[A-Za-z_][A-Za-z0-9_]*=/ {
			name = $0; sub(/=.*/, "", name)
			if (name in names) printf "%s%s\n\n", block, $0
		}
		{ block = "" }
	' "$1"
}

main() {
	check=false yes=false backup=true restore='' target=''
	while [ $# -gt 0 ]; do
		case $1 in
		--check) check=true ;;
		-y | --yes) yes=true ;;
		--no-backup) backup=false ;;
		--restore)
			[ $# -gt 1 ] || fail "--restore needs a backup"
			restore=$2
			shift
			;;
		-h | --help) usage && exit 0 ;;
		-*) usage >&2 && exit 2 ;;
		*)
			target=${1#v}
			[[ $target =~ $semver ]] || fail "not a release (X.Y.Z): $1"
			;;
		esac
		shift
	done

	# A release's own update.sh is run from a temporary directory: RUNSTEN_DIR names the
	# installation then.
	dir=${RUNSTEN_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
	cd "$dir"
	[ -z "$restore" ] || [ -f "$restore" ] || fail "no backup $restore"
	[ -z "$restore" ] || [ -n "$target" ] || fail "--restore needs the release the backup was taken on"
	[ -f compose.yaml ] && [ -f .env ] || fail "no compose.yaml and .env in $dir: run the update.sh of the installation"
	command -v curl >/dev/null || fail "curl is needed"
	docker compose version >/dev/null 2>&1 || fail "Docker Compose is needed"

	current=$(running)
	if [ -z "$target" ] || $check; then
		newest=$(latest)
		[[ $newest =~ $semver ]] || fail "cannot read the latest release from GitHub"
		target=${target:-$newest}
	fi

	if $check; then
		if [ -z "$current" ]; then
			echo "Runsten $newest is the latest release; the one running is unknown (RUNSTEN_VERSION is not a release)."
		elif newer "$newest" "$current"; then
			echo "Runsten $newest is out, $current runs: $releases/tag/v$newest"
		else
			echo "Runsten $current is the latest release."
		fi
		exit 0
	fi
	if [ "$target" = "$current" ] && [ -z "$restore" ]; then
		echo "Runsten $current runs already."
		exit 0
	fi

	# In the installation's directory, so that each file is replaced at once (a rename),
	# this script included.
	tmp=$(mktemp -d "$dir/.update.XXXXXX")
	trap 'rm -rf "$tmp" ${RUNSTEN_UPDATE_PREVIOUS:+"$RUNSTEN_UPDATE_PREVIOUS"}' EXIT

	# A newer release's update.sh knows its files and checks: it goes on in its stead.
	# The way back is this one's.
	if [ -z "${RUNSTEN_UPDATE_PREVIOUS:-}" ] && { [ -z "$current" ] || newer "$target" "$current"; } &&
		curl -fsSL --create-dirs -o "$tmp/deploy/update.sh" "$raw/v$target/deploy/update.sh" 2>/dev/null &&
		! cmp -s "$tmp/deploy/update.sh" "${BASH_SOURCE[0]}"; then
		args=()
		$yes && args+=(--yes)
		$backup || args+=(--no-backup)
		trap - EXIT
		RUNSTEN_DIR=$dir RUNSTEN_UPDATE_PREVIOUS=$tmp exec bash "$tmp/deploy/update.sh" "${args[@]}" "$target"
	fi

	echo "Fetching the files of Runsten $target."
	mkdir -p "$tmp/new"
	for f in "${files[@]}"; do
		curl -fsSL --create-dirs -o "$tmp/new/$f" "$raw/v$target/$f" ||
			fail "cannot fetch $f of $target: is v$target a release ($releases)?"
	done
	# The way back to a release older than this script keeps it.
	curl -fsSL --create-dirs -o "$tmp/new/deploy/update.sh" "$raw/v$target/deploy/update.sh" 2>/dev/null ||
		rm -f "$tmp/new/deploy/update.sh"

	# A variable this release adds and .env lacks: the person settles it, not the script.
	added=$(variables "$tmp/new/.env.example")
	if [ -f .env.example ]; then added=$(comm -23 <(echo "$added") <(variables .env.example)); fi
	missing=$(comm -23 <(echo "$added") <(variables .env))
	if [ -n "$missing" ]; then
		echo
		echo "Runsten $target adds to .env.example what .env lacks:"
		echo
		explain "$tmp/new/.env.example" "$missing"
		fail "nothing done: add them to .env (empty when not needed), then run it again"
	fi

	# Without the target's notes, a rebuild they ask for would go unnoticed.
	target_notes=$(notes "$target") || {
		echo "The update notes of $target cannot be read: https://runsten.app/docs/self-hosting/updating#update-notes"
		$yes && fail "nothing done: read them, then run it again"
	}
	echo
	if [ -n "$current" ]; then
		echo "Runsten $current → $target"
		if newer "$target" "$current"; then
			echo "The notes of each release since $current: $releases"
		elif [ -z "$restore" ]; then
			echo "An older release, on the database of $current: --restore a backup taken on $target to have its own."
		fi
		# Those of the release running unread, all the target's are shown.
		current_notes=$(notes "$current") || current_notes=''
		added_notes=$(grep -vxF -f <(echo "$current_notes") <<<"$target_notes" || true)
	else
		echo "Runsten → $target (the release running is unknown: RUNSTEN_VERSION is not a release)"
		echo "Its notes: $releases/tag/v$target"
		added_notes=$target_notes
	fi
	rebuild=false
	if [ -n "$added_notes" ]; then
		echo
		echo "Update notes (https://runsten.app/docs/self-hosting/updating#update-notes):"
		echo "${added_notes//\*\*/}"
		grep -q 'Rebuild after updating' <<<"$added_notes" && rebuild=true
	fi
	[ -z "$restore" ] || echo "The database will be replaced with $restore: what was read since it was taken is lost."
	if $rebuild; then
		echo
		echo "A note asks for a rebuild of the trips and charges: it follows the update, the collector stopped meanwhile."
	fi
	echo
	confirm "Update to $target?" || fail "nothing done"

	echo "Pulling the images of $target."
	RUNSTEN_VERSION=$target docker compose --project-directory "$dir" -f "$tmp/new/compose.yaml" pull ||
		fail "nothing done: the images of $target cannot be pulled"

	dump=
	if $backup; then
		mkdir -p backups
		chmod 700 backups
		dump=backups/runsten-${current:-unknown}-$(date +%Y%m%d-%H%M%S).dump
		echo "Backing the database up into $dump."
		(umask 077 && docker compose exec -T postgres pg_dump -U postgres --format=custom runsten >"$dump") ||
			{ rm -f "$dump"; fail "nothing done: no backup (is postgres running? --no-backup to go on without one)"; }
	fi

	# Into a new database: the migrations of the newer release are not undone otherwise.
	if [ -n "$restore" ]; then
		echo "Restoring $restore."
		docker compose stop api collector
		docker compose exec -T postgres dropdb -U postgres --force runsten
		docker compose exec -T postgres createdb -U postgres --owner=runsten runsten
		docker compose exec -T postgres pg_restore -U postgres --dbname=runsten \
			--single-transaction --exit-on-error <"$restore" ||
			fail "the restore failed: the database is empty${dump:+, $dump holds it as it was}; api and collector are stopped"
	fi

	for f in "${files[@]}" deploy/update.sh; do
		[ -f "$tmp/new/$f" ] || continue
		mkdir -p "$(dirname "$f")"
		mv -f "$tmp/new/$f" "$f"
	done
	chmod +x deploy/update.sh
	if grep -q '^RUNSTEN_VERSION=' .env; then
		sed -i.bak "s/^RUNSTEN_VERSION=.*/RUNSTEN_VERSION=$target/" .env && rm -f .env.bak
	else
		printf '\nRUNSTEN_VERSION=%s\n' "$target" >>.env
	fi

	# A RUNSTEN_VERSION of the shell would win over .env's.
	export RUNSTEN_VERSION=$target
	echo "Starting Runsten $target."
	docker compose up -d --wait

	if $rebuild; then
		if confirm "Rebuild the trips and charges now?"; then
			docker compose stop collector
			docker compose run --rm collector rebuild
			docker compose start collector
		else
			echo "To rebuild later: https://runsten.app/docs/self-hosting/backup#rebuild"
		fi
	fi

	echo
	echo "Runsten $target runs."
	if [ -n "$dump" ] && [ -n "$current" ] && newer "$target" "$current"; then
		echo "The way back to $current, its database as before: deploy/update.sh --restore $dump $current"
	fi
}

main "$@"
