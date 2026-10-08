#!/usr/bin/env bash
# End-to-end test of compose.yaml against the simulator (sim.env): start the stack,
# create the first user, sign in, connect the simulated Volvo ID under that session,
# read the vehicle's state, trips (a reconstructed one included), charges and statistics
# through the API, run the browser tests of the web interface (Playwright and axe, scripts/e2e.sh),
# set the account's MQTT broker to the mqtt profile's Mosquitto and read what the
# collector publishes there (the state, Home Assistant's discovery) until the broker is
# removed, give the DC charge a cost (a tariff, then an entered one), then back up,
# restore and rebuild as documentation/self-hosting/backup.md describes, the entered
# cost kept. Everything but the first checks goes through runsten-web, as a browser
# does: its relay to runsten-api is under test too. Everything is removed at the end,
# volume included. Its own Compose project: a running sim stack is left alone, but its
# ports (8080, 8081, 8082, 8090, 1883) must be free. Needs curl, jq, web/node_modules
# (task web-install) and PLAYWRIGHT_IMAGE (the Taskfile sets it).
set -euo pipefail
cd "$(dirname "$0")/.."

# A short errand: the way there hidden by an API outage, a DC charge, the way back. A
# reconstructed trip, a charge and a trip within two minutes of the start.
export RUNSTEN_SIM_SCENARIO=errand
project=runsten-compose-test

# --profile replaces sim.env's COMPOSE_PROFILES: both are named.
compose() { docker compose --env-file sim.env --project-name "$project" --profile sim --profile mqtt "$@"; }
sql() { compose exec -T postgres psql -U postgres -d runsten -tAc "$1"; }
fail() { echo "compose-test: $*" >&2; exit 1; }

api=http://127.0.0.1:8082   # runsten-web, relaying to runsten-api
direct=http://127.0.0.1:8081 # runsten-api alone
user=admin
password=runsten-dev-password # development only, like sim.env

dump=$(mktemp)
jar=$(mktemp)
cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then compose logs --no-color --tail=40 >&2 || true; fi
	compose down --volumes --remove-orphans >/dev/null 2>&1 || true
	rm -f "$dump" "$jar"
	exit "$status"
}
trap cleanup EXIT

# eventually query: waits up to 60 s for query to return true.
eventually() {
	for _ in $(seq 60); do
		[ "$(sql "$1")" = t ] && return 0
		sleep 1
	done
	fail "still false after 60 s: $1"
}

# status curl-args…: the HTTP status of a request, without session.
status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

# get path: a JSON response of the API, with the session.
get() { curl -fsS -b "$jar" "$api$1"; }

# send method path body: a JSON request of the API, with the session.
send() { curl -fsS -b "$jar" -X "$1" -H 'Content-Type: application/json' -d "$3" "$api$2"; }

# costs_add_up: the costs of the statistics are the sums of those of the list.
costs_add_up() {
	jq -en --argjson s "$(get "/api/v1/vehicles/$vehicle/stats")" \
		--argjson c "$(get "/api/v1/vehicles/$vehicle/charges?limit=200")" '
		$s.currency.code == "EUR"
		and $s.totals.charges.cost.min_minor == ([$c.items[].cost.min_minor // 0] | add)
		and $s.totals.charges.cost.max_minor == ([$c.items[].cost.max_minor // 0] | add)
		and $s.totals.charges.cost_unknown == ([$c.items[] | select(.cost == null)] | length)
		and $s.totals.charges.cost_entered == ([$c.items[] | select(.cost.source == "entered")] | length)' >/dev/null ||
		fail "the cost of the statistics differs from the list: $(get "/api/v1/vehicles/$vehicle/stats")"
}

# capacities_from_api: the scenario's family (EX-SIM) is not in the catalog, so every
# energy rests on the capacity the vendor reports, not on a net capacity.
capacities_from_api() {
	jq -en --argjson s "$(get "/api/v1/vehicles/$vehicle/state")" \
		--argjson t "$(get "/api/v1/vehicles/$vehicle/trips?limit=200")" \
		--argjson c "$(get "/api/v1/vehicles/$vehicle/charges?limit=200")" '
		[$t.items[], $c.items[]] as $events
		| ($events | length) >= 3
		and all($events[]; .capacity_source == "api" and .capacity_kwh == $s.battery_capacity_kwh.value)' >/dev/null ||
		fail "energies not on the vendor's capacity: $(get "/api/v1/vehicles/$vehicle/trips?limit=200")"
}

# retained: what the broker holds, as {topic: payload}, read by a subscriber in its
# container for 2 seconds: the retained messages, then any published meanwhile (the
# latest wins; an empty one removes the topic).
retained() {
	compose exec -T mosquitto mosquitto_sub -t '#' -F '%t %p' -W 2 2>/dev/null |
		jq -Rn '[inputs | capture("^(?<t>[^ ]+) (?<p>.*)$") | {(.t): .p}] | add // {} | with_entries(select(.value != ""))'
}

# until_mqtt filter: waits up to 120 s for the jq filter to be true on retained, given
# the vehicle's topics as $v and its API state as $s.
until_mqtt() {
	for _ in $(seq 40); do
		jq -e --arg v "runsten/vehicles/$vehicle/" --argjson s "$(get "/api/v1/vehicles/$vehicle/state")" "$1" \
			<<<"$(retained)" >/dev/null && return 0
		sleep 1
	done
	fail "still false after 120 s: $1, on $(retained)"
}

# until_api path filter: waits up to 150 s for the jq filter to be true on path.
until_api() {
	for _ in $(seq 150); do
		[ "$(get "$1" | jq -r "$2")" = true ] && return 0
		sleep 1
	done
	fail "still false after 150 s: $1 | $2"
}

echo "→ start (healthchecks included)"
# A run killed before its cleanup leaves its stack behind.
compose down --volumes --remove-orphans >/dev/null 2>&1 || true
compose up --detach --build --wait --wait-timeout 180

[ "$(sql "SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname = 'runsten'")" = f ] ||
	fail "the application user must be neither superuser nor CREATEROLE"
case "$(compose ps postgres --format '{{.Ports}}')" in *'->'*) fail "PostgreSQL must not be published" ;; esac

echo "→ first user"
printf '%s\n' "$password" | compose run --rm --no-deps -T api user create "$user"
[ "$(sql 'SELECT count(*) FROM users')" = 1 ] || fail "no user created"
if printf '%s\n' "$password" | compose run --rm --no-deps -T api user create second 2>/dev/null; then
	fail "a second user was created in the single account"
fi

echo "→ runsten-web: the app, with its CSP; runsten-api alone still answers"
page=$(curl -fsS -D - "$api/vehicles/any")
grep -qi "^content-security-policy: default-src 'none'; script-src 'self'; style-src 'self' 'nonce-" <<<"$page" ||
	fail "the app is served without its CSP"
grep -q '<div id="app">' <<<"$page" || fail "the app's page is missing"
[ "$(status "$direct/login")" = 200 ] || fail "runsten-api's own login page is gone"

echo "→ nothing without a session"
[ "$(status "$api/api/v1/vehicles")" = 401 ] || fail "the API answers without a session"
[ "$(status "$api/auth/volvo/start")" = 303 ] || fail "/auth/volvo/start must send to the login without a session"
[ "$(status -H 'Content-Type: application/json' -d "{\"username\":\"$user\",\"password\":\"wrong password, surely\"}" \
	"$api/api/v1/session")" = 401 ] || fail "a wrong password was accepted"
[ "$(status -H 'Content-Type: application/json' -H 'Origin: https://attacker.example' \
	-d "{\"username\":\"$user\",\"password\":\"$password\"}" "$api/api/v1/session")" = 403 ] ||
	fail "a cross-origin login was accepted"

echo "→ sign in"
curl -fsS -c "$jar" -H 'Content-Type: application/json' -d "{\"username\":\"$user\",\"password\":\"$password\"}" \
	"$api/api/v1/session" | jq -e --arg u "$user" '.user.username == $u' >/dev/null || fail "login"

echo "→ OAuth connection through runsten-web, under the session (the callback is on it)"
curl -fsS -L -b "$jar" -c "$jar" --connect-to simulator:8080:127.0.0.1:8080 -o /dev/null "$api/auth/volvo/start"
[ "$(sql 'SELECT count(*) FROM connections WHERE reauth_at IS NULL')" = 1 ] || fail "no active connection"
[ "$(sql 'SELECT count(*) FROM connections c JOIN users u USING (account_id)')" = 1 ] ||
	fail "the connection is not in the user's account"

echo "→ snapshots"
eventually 'SELECT count(*) > 0 FROM snapshots'

echo "→ Verdandi: vehicles and current state"
vehicle=$(get /api/v1/vehicles | jq -er '.items | select(length == 1) | .[0] | select(.connection.status == "active") | .id') ||
	fail "one vehicle with an active connection expected"
until_api "/api/v1/vehicles/$vehicle/state" '.soc_pct.value != null and .odometer_km.value != null and .checked_at != null'
# The scenario's family is not in the catalog: read, but no variant recognized, none to
# choose, and a variant of another family refused.
until_api "/api/v1/vehicles/$vehicle" '.model == {"family": "EX-SIM", "model_year": 2026, "variant": null, "variant_source": null, "ac_max_kw": null}'
get "/api/v1/vehicles/$vehicle/variants" | jq -e '.items == []' >/dev/null || fail "variants offered for EX-SIM"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$jar" -X PUT -H 'Content-Type: application/json' \
	-d '{"variant_id":"ex30-er-2024","ac_max_kw":null}' "$api/api/v1/vehicles/$vehicle/model")" = 400 ] ||
	fail "a variant of another family accepted"

echo "→ Urd: a trip, a charge, and the way there, never seen driving, reconstructed"
until_api "/api/v1/vehicles/$vehicle/trips" 'any(.items[]; .reconstructed | not) and any(.items[]; .reconstructed)'
until_api "/api/v1/vehicles/$vehicle/charges" '.items | length >= 1'
trip=$(get "/api/v1/vehicles/$vehicle/trips" | jq -c '[.items[] | select(.reconstructed | not)] | last')
echo "$trip" | jq -e '.start.after != null and .start.before != null
	and .end.after != null and .end.before != null and (.distance_km | . > 11 and . < 13)' >/dev/null ||
	fail "unexpected trip: $trip"
trip_id=$(echo "$trip" | jq -r .id)
[ "$(get "/api/v1/vehicles/$vehicle/trips/$trip_id" | jq -c .)" = "$trip" ] || fail "trip detail differs from the list"
get "/api/v1/vehicles/$vehicle/charges" | jq -e '.items[-1] | .type == "DC" and .end_soc_pct >= 79' >/dev/null ||
	fail "unexpected charge: $(get "/api/v1/vehicles/$vehicle/charges")"
get "/api/v1/vehicles/$vehicle/trips?limit=1" | jq -e '.items | length == 1' >/dev/null || fail "pagination"
reconstructed=$(get "/api/v1/vehicles/$vehicle/trips" | jq -c '[.items[] | select(.reconstructed)] | first')
# Only an interval is known: the start and end bounds are the same. Times are compared
# as times, their fractions of a second (variable length) left out.
echo "$reconstructed" | jq -e 'def t: sub("\\.[0-9]+Z$"; "Z") | fromdate;
	.start == .end and (.start.after | t) < (.end.before | t)
	and (.distance_km | . > 5 and . < 7)' >/dev/null || fail "unexpected reconstructed trip: $reconstructed"
capacities_from_api

echo "→ statistics: the totals are the sums of the lists, and each event counts once by day"
jq -en --argjson s "$(get "/api/v1/vehicles/$vehicle/stats?bucket=day")" \
	--argjson t "$(get "/api/v1/vehicles/$vehicle/trips?limit=200")" \
	--argjson c "$(get "/api/v1/vehicles/$vehicle/charges?limit=200")" '
	def t: sub("\\.[0-9]+Z$"; "Z") | fromdate;
	def near($a; $b): ($a - $b) | fabs < 1e-6;
	$s.totals.trips.count == ($t.items | length) and $s.totals.trips.count >= 2
	and $s.totals.trips.reconstructed == ([$t.items[] | select(.reconstructed)] | length)
	and near($s.totals.trips.distance_km; [$t.items[].distance_km // 0] | add)
	and $s.totals.charges.count == ($c.items | length)
	and $s.totals.charges.by_type.dc.count == ([$c.items[] | select(.type == "DC")] | length)
	and ([$s.buckets[].trips.count] | add) == $s.totals.trips.count
	and ($s.buckets | length) == (($s.to | t) / 86400 | ceil) - (($s.from | t) / 86400 | floor)' >/dev/null ||
	fail "statistics differ from the lists: $(get "/api/v1/vehicles/$vehicle/stats?bucket=day")"

echo "→ the web interface in a browser (Playwright, axe)"
scripts/e2e.sh "${project}_default"

echo "→ MQTT: the state and Home Assistant's discovery on the broker set in the settings, all removed with it"
send PUT /api/v1/mqtt '{"url":"mqtt://mosquitto:1883","username":"","password":"","client_id":"","topic_prefix":"runsten",
	"discovery":true,"discovery_prefix":"homeassistant","publish_location":true}' |
	jq -e '.broker.url == "mqtt://mosquitto:1883" and .public_only == false' >/dev/null || fail "MQTT broker"
until_api /api/v1/mqtt '.status.connected_at != null and .status.failure == null'
# The errand is over: the vehicle stays parked, and the topics tell what the API reads.
until_mqtt '.["runsten/status"] == "online"
	and (.[$v + "battery_level"] | tonumber) == $s.soc_pct.value
	and (.[$v + "odometer_km"] | tonumber) == $s.odometer_km.value
	and (.[$v + "range_km"] | tonumber) == $s.range_km.value
	and .[$v + "engine"] == $s.engine.value and .[$v + "charging_status"] == $s.charging.status.value
	and (.[$v + "read_at"] | test("^[0-9-]{10}T[0-9:.]{8,}Z$"))
	and (.[$v + "location"] | fromjson) == {latitude: $s.position.value.lat, longitude: $s.position.value.lon}
	and ([to_entries[] | select(.key | startswith("homeassistant/"))] as $configs
		| ($configs | length) == 12
		and all($configs[]; (.key | test("^homeassistant/(sensor|binary_sensor|device_tracker)/runsten_[^/]+/[a-z_]+/config$"))
			and (.value | fromjson | .device.identifiers == [$v | split("/")[2] | "runsten_" + .]
				and .availability_topic == "runsten/status" and .device.name == "EX-SIM · 2026")))'
[ "$(status -b "$jar" -X DELETE "$api/api/v1/mqtt")" = 204 ] || fail "MQTT broker not removed"
until_mqtt '. == {}'

echo "→ costs: a currency, a place at the DC charge with a fixed price, then an entered cost"
send PUT /api/v1/settings '{"currency":"EUR"}' | jq -e '.currency.code == "EUR"' >/dev/null || fail "currency"
charge=$(get "/api/v1/vehicles/$vehicle/charges" | jq -c '[.items[] | select(.type == "DC")] | first')
charge_id=$(echo "$charge" | jq -r .id)
place=$(echo "$charge" | jq -c '{name: "Station", position: .position, radius_m: 100, time_zone: "Europe/Paris",
	without_position: false, max_power_kw: null, efficiency: null,
	tariff: [{valid_from: "2000-01-01", price_per_kwh: 0.5, windows: []}]}')
send POST /api/v1/places "$place" | jq -e '.name == "Station"' >/dev/null || fail "place"
# One price over the whole window: a single amount, the energy from the grid at 0.50 €,
# with the default DC efficiency.
charge=$(get "/api/v1/vehicles/$vehicle/charges/$charge_id")
echo "$charge" | jq -e '(.energy_soc_kwh / 0.95 * 0.5 * 100 | round) as $cost
	| .place.name == "Station" and .cost.source == "tariff" and .cost.currency == "EUR" and .cost.efficiency == 0.95
	and .cost.min_minor == $cost and .cost.max_minor == $cost' >/dev/null || fail "unexpected cost: $charge"
costs_add_up
send PUT "/api/v1/vehicles/$vehicle/charges/$charge_id/cost" '{"amount_minor":1234,"energy_kwh":null,"note":"compose-test"}' |
	jq -e '.cost.source == "entered" and .cost.min_minor == 1234 and .cost.max_minor == 1234' >/dev/null ||
	fail "entered cost"
costs_add_up

echo "→ backup, restore, rebuild"
# Stopped first: a refresh between the dump and the stop would rotate the refresh token,
# and the restore would bring back one the token endpoint has already seen used.
compose stop api collector
stored=$(sql 'SELECT count(*) FROM snapshots')
checked=$(sql 'SELECT max(checked_at) FROM snapshots')
echo "  $stored snapshots"
compose exec -T postgres pg_dump -U postgres --format=custom runsten >"$dump"
compose exec -T postgres pg_restore -U postgres --dbname=runsten --clean --if-exists \
	--single-transaction --exit-on-error <"$dump"
[ "$(sql 'SELECT count(*) FROM snapshots')" = "$stored" ] || fail "restored snapshot count differs"
compose run --rm --no-deps collector rebuild
compose up --detach --wait --wait-timeout 60 api collector web
# The collector polls again with the restored, still decryptable, tokens.
eventually "SELECT max(checked_at) > '$checked' FROM snapshots"
[ "$(sql 'SELECT count(*) FROM connections WHERE reauth_at IS NULL')" = 1 ] ||
	fail "the connection did not survive the restore"
# The session is in the database: it survives the restart and the restore. The trip
# keeps its ID through the rebuild.
[ "$(get "/api/v1/vehicles/$vehicle/trips/$trip_id" | jq -r .id)" = "$trip_id" ] ||
	fail "the trip changed ID through the rebuild"
# The entered cost is not derived: the rebuild keeps it, attached to its charge.
get "/api/v1/vehicles/$vehicle/charges/$charge_id" |
	jq -e '.cost.source == "entered" and .cost.min_minor == 1234 and .cost.note == "compose-test"' >/dev/null ||
	fail "the entered cost did not survive the rebuild: $(get "/api/v1/vehicles/$vehicle/charges/$charge_id")"
get /api/v1/charge-costs/orphans | jq -e '.items == []' >/dev/null || fail "orphaned costs after the rebuild"
costs_add_up
capacities_from_api

echo "→ sign out"
[ "$(status -b "$jar" -X DELETE "$api/api/v1/session")" = 204 ] || fail "logout"
[ "$(status -b "$jar" "$api/api/v1/session")" = 401 ] || fail "the session survived the logout"

echo "compose-test: ok, in ${SECONDS} s"
