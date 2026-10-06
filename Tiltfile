# Development stack: the simulator, runsten-api, the collector and the front end's dev
# server (Vite, hot reload), each through its task, with PostgreSQL, the development user,
# the simulated Volvo ID's connection, and an MQTT broker (Mosquitto) the collector
# publishes to. `tilt up`, then http://localhost:10350; another
# scenario: `tilt up -- --scenario=missed-trip` (errand,ex30: two vehicles), or `tilt args
# -- --scenario=ex30` while it runs. Ctrl-C stops them all (tilt down has nothing to remove);
# PostgreSQL (task db-up) keeps its data, task db-down removes it. Needs curl, and ports
# 8080, 8081, 8090, 5173 and 1883 free (not with task compose-sim running).
# No Kubernetes: local resources only.

config.define_string('scenario', usage='scenario(s) of scenarios/, comma-separated (default commute)')
scenario = config.parse().get('scenario', 'commute')

# An installed task, or the one pinned in tools/go.mod, as the CI runs it. No local() at
# load: Tilt refuses it under a Kubernetes context that looks like production, and none is used.
installed = [d for d in os.getenv('PATH', '').split(':') if d and os.path.exists(d + '/task')]
task = 'task -s' if installed else 'go tool -modfile=tools/go.mod task -s'

# A binary restarts when a package it is built from (go list -deps ./cmd/...) changes,
# not when its tests do.
go_mod = ['go.mod', 'go.sum']
go_ignore = ['**/*_test.go', '**/testdata']
# runsten-api and runsten-collector share most of internal/, but neither the simulator nor
# runsten-web's handler.
backend_ignore = go_ignore + ['internal/simulator', 'internal/web', 'internal/archtest']

# wait_for url: up to 3 minutes, the first go run compiles.
def wait_for(url):
    return ('i=0; until curl -fsS -o /dev/null {0} 2>/dev/null; do i=$((i+1)); '
            + '[ $i -lt 180 ] || {{ echo "no answer from {0}"; exit 1; }}; sleep 1; done; ').format(url)

# Every 5 seconds, for as long as the stack runs: the simulator logs each request.
def http_probe(port, path):
    return probe(period_secs=5, http_get=http_get_action(port=port, host='127.0.0.1', path=path))

# Once each; their tasks are asked for again by the services, which find them done.
local_resource('postgres', cmd=task + ' db-up', labels=['infra'])
local_resource('web-install', cmd=task + ' web-install',
               deps=['web/package.json', 'web/package-lock.json', 'web/.nvmrc'], labels=['frontend'])

# The simulator starts afresh each time: the tokens of a previous run are worthless. Each
# start (a change, a trigger, another scenario) writes bin/tilt/sim-started, which dev-user
# and connect-sim watch. Written by the serve_cmd, not by an update's cmd: Tilt runs it once
# the previous simulator has exited, so connect-sim cannot reach the old one.
local_resource('sim', serve_cmd='mkdir -p bin/tilt && date > bin/tilt/sim-started && ' + task + ' run-sim SCENARIO=' + scenario,
               deps=['cmd/runsten-simulator', 'internal/simulator', 'internal/platform', 'scenarios'] + go_mod,
               ignore=go_ignore,
               readiness_probe=http_probe(8080, '/sim/status'),
               links=[link('http://127.0.0.1:8080/sim/status', 'Simulator (' + scenario + ')')],
               labels=['backend'])

local_resource('api', serve_cmd=task + ' run-api',
               deps=['cmd/runsten-api', 'internal'] + go_mod, ignore=backend_ignore, resource_deps=['postgres'],
               readiness_probe=http_probe(8081, '/healthz'), labels=['backend'])

# runsten-api migrates the database when it starts: the user is created after it answers.
local_resource('dev-user', cmd=task + ' dev-user >/dev/null 2>&1 || echo "user admin not created (it may exist already)"',
               deps=['bin/tilt/sim-started'], resource_deps=['api'], labels=['backend'])

local_resource('connect-sim', cmd=wait_for('http://127.0.0.1:8080/sim/status') + task + ' connect-sim >/dev/null'
                   + ' || { echo "the simulated Volvo ID could not be connected"; exit 1; }',
               deps=['bin/tilt/sim-started'], resource_deps=['sim', 'api', 'dev-user'], labels=['backend'])

# Mosquitto, in a container removed when it stops; the user's broker is set to it once
# runsten-api answers. mqtt-sub prints its messages, when started from Tilt's page.
local_resource('mosquitto', serve_cmd=task + ' run-mqtt', deps=['deploy/mosquitto'],
               readiness_probe=probe(period_secs=5, tcp_socket=tcp_socket_action(port=1883, host='127.0.0.1')),
               labels=['infra'])
local_resource('dev-mqtt', cmd=task + ' dev-mqtt', resource_deps=['api', 'dev-user', 'mosquitto'], labels=['backend'])
local_resource('mqtt-sub', serve_cmd=task + ' mqtt-sub', resource_deps=['mosquitto'],
               auto_init=False, trigger_mode=TRIGGER_MODE_MANUAL, labels=['infra'])

local_resource('collector', serve_cmd=task + ' run-collector',
               deps=['cmd/runsten-collector', 'internal'] + go_mod, ignore=backend_ignore, resource_deps=['connect-sim'],
               readiness_probe=http_probe(8090, '/'),
               links=[link('http://127.0.0.1:8090', 'Collector (debug page)')], labels=['backend'])

# Vite reloads the sources itself: it restarts only once npm ci has rewritten node_modules.
local_resource('vite', serve_cmd=task + ' run-web',
               deps=['web/node_modules/.package-lock.json'], resource_deps=['web-install', 'api'],
               readiness_probe=http_probe(5173, '/'),
               links=[link('http://127.0.0.1:5173', 'Runsten (admin / runsten-dev-password)')],
               labels=['frontend'])
