// npm audit, minus the advisories listed in IGNORED.
// Usage: node scripts/audit.mjs <moderate|high|critical> [--omit=dev]
//
// npm audit has no way to ignore an advisory: this runs it in JSON, drops every package whose
// only reasons to be vulnerable are ignored advisories, and fails if a package at or above the
// level remains. Each entry says why it is ignored; remove it once a fixed version exists.
import { execFileSync } from 'node:child_process'

const IGNORED = {
  // picomatch <= 4.0.7, ReDoS. No fixed version yet. Build-time only (vite, vue-router's
  // build plugins): nothing of it ships in the bundle served to the browser.
  'GHSA-fc5q-39xc-vjpf': 'picomatch ReDoS, no fixed version, build-time only',
}

const LEVELS = ['info', 'low', 'moderate', 'high', 'critical']
const [level, ...npmArgs] = process.argv.slice(2)
if (!LEVELS.includes(level)) {
  console.error(`usage: audit.mjs <${LEVELS.join('|')}> [npm audit args]`)
  process.exit(2)
}

let out
try {
  out = execFileSync('npm', ['audit', '--json', ...npmArgs], { encoding: 'utf8' })
} catch (e) {
  out = e.stdout // npm audit exits non-zero when it finds anything
}
const vulns = JSON.parse(out).vulnerabilities ?? {}

const ghsa = (url) => url.split('/').pop()
// The worst severity a package is vulnerable to through advisories not ignored, -1 if none.
function worst(name, seen = new Set()) {
  if (seen.has(name) || !vulns[name]) return -1
  seen.add(name)
  return Math.max(
    -1,
    ...vulns[name].via.map((via) =>
      typeof via === 'string'
        ? worst(via, seen)
        : ghsa(via.url) in IGNORED
          ? -1
          : LEVELS.indexOf(via.severity),
    ),
  )
}

const min = LEVELS.indexOf(level)
const failing = Object.values(vulns).filter((v) => worst(v.name) >= min)
const ignored = Object.values(vulns).filter((v) => worst(v.name) < 0)

for (const v of ignored) console.log(`ignored: ${v.name} (${v.severity})`)
for (const v of failing) console.error(`vulnerable: ${v.name} (${v.severity})`)
if (failing.length) {
  console.error(`${failing.length} package(s) at ${level} or above: run npm audit for details`)
  process.exit(1)
}
console.log(
  `npm audit: nothing at ${level} or above, ${Object.keys(IGNORED).length} advisory ignored`,
)
