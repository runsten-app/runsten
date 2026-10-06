import type { MqttBroker, MqttBrokerUpdate } from '@/entities/mqtt'

// The API's bounds of a broker (PUT /mqtt): change them with it.
export const mqttLimits = { url: 255, username: 256, password: 256, clientId: 64, prefix: 64 }

// The defaults a new broker is offered: Runsten's topics, and Home Assistant's
// discovery prefix unless changed in its MQTT integration.
export const defaults = { topicPrefix: 'runsten', discoveryPrefix: 'homeassistant' }

// field names the elements that show each issue: the error summary leads there.
export const field = {
  url: 'mqtt-url',
  username: 'mqtt-username',
  password: 'mqtt-password',
  clientId: 'mqtt-client-id',
  topicPrefix: 'mqtt-topic-prefix',
  discoveryPrefix: 'mqtt-discovery-prefix',
} as const

// Draft is the form as typed. password empty: the stored one is kept for the same host,
// unless removePassword.
export type Draft = {
  url: string
  username: string
  password: string
  removePassword: boolean
  clientId: string
  topicPrefix: string
  discovery: boolean
  discoveryPrefix: string
  publishLocation: boolean
}

export type Rule =
  | 'required'
  | 'scheme'
  | 'tls'
  | 'host'
  | 'credentials'
  | 'path'
  | 'port'
  | 'tooLong'
  | 'passwordAgain'
  | 'prefix'
  | 'clientId'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }
export type Checked = { issues: Issue[]; body: MqttBrokerUpdate | null }

// draftOf is the form for a broker, or for a new one: the password is never known.
export function draftOf(b: MqttBroker | null): Draft {
  return {
    url: b?.url ?? '',
    username: b?.username ?? '',
    password: '',
    removePassword: false,
    clientId: b?.client_id ?? '',
    topicPrefix: b?.topic_prefix ?? defaults.topicPrefix,
    discovery: b?.discovery ?? true,
    discoveryPrefix: b?.discovery_prefix ?? defaults.discoveryPrefix,
    publishLocation: b?.publish_location ?? true,
  }
}

// hostOf is a broker URL's host, lowercase as the API compares it: a URL of another
// scheme keeps its case (WHATWG). Null: no URL of a broker.
export function hostOf(url: string): string | null {
  try {
    const u = new URL(url)
    return u.hostname ? u.hostname.toLowerCase().replace(/^\[|\]$/g, '') : null
  } catch {
    return null
  }
}

const topicLevels = /^[^/+#]+(\/[^/+#]+)*$/
const length = (s: string) => [...s].length

function checkURL(url: string, publicOnly: boolean): Issue | null {
  const at = (rule: Rule, params?: Record<string, number>): Issue => ({
    field: field.url,
    rule,
    ...(params && { params }),
  })
  if (!url) return at('required')
  if (length(url) > mqttLimits.url) return at('tooLong', { max: mqttLimits.url })
  const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(url)?.[1]?.toLowerCase()
  if (scheme !== 'mqtt' && scheme !== 'mqtts') return at('scheme')
  let u: URL
  try {
    u = new URL(url)
  } catch {
    // An invalid port is the most common cause, as mqtt://host:99999.
    return /:\d+\/?$/.test(url) ? at('port') : at('host')
  }
  if (!u.hostname || !url.slice(scheme.length + 1).startsWith('//')) return at('host')
  if (u.username || u.password) return at('credentials')
  if ((u.pathname && u.pathname !== '/') || u.search || u.hash) return at('path')
  if (u.port === '0') return at('port')
  if (publicOnly && scheme !== 'mqtts') return at('tls')
  return null
}

function checkPrefix(id: string, value: string): Issue | null {
  if (!value) return { field: id, rule: 'required' }
  if (length(value) > mqttLimits.prefix)
    return { field: id, rule: 'tooLong', params: { max: mqttLimits.prefix } }
  if (!topicLevels.test(value)) return { field: id, rule: 'prefix' }
  return null
}

// validate checks the form with the API's rules, and gives the body to send. The
// password the broker has is kept for the same host only, as the API does: for
// another, it is given again, or removed.
export function validate(d: Draft, stored: MqttBroker | null, publicOnly: boolean): Checked {
  const url = d.url.trim()
  const clientId = d.clientId.trim()
  const topicPrefix = d.topicPrefix.trim()
  const discoveryPrefix = d.discoveryPrefix.trim()
  const issues: Issue[] = []
  const add = (i: Issue | null) => i && issues.push(i)

  add(checkURL(url, publicOnly))
  if (length(d.username) > mqttLimits.username)
    add({ field: field.username, rule: 'tooLong', params: { max: mqttLimits.username } })
  let password: string | null = d.removePassword ? '' : d.password
  if (length(d.password) > mqttLimits.password)
    add({ field: field.password, rule: 'tooLong', params: { max: mqttLimits.password } })
  else if (!d.password && !d.removePassword && stored?.password_set) {
    if (hostOf(url) === hostOf(stored.url)) password = null
    else if (!issues.some((i) => i.field === field.url))
      add({ field: field.password, rule: 'passwordAgain' })
  }
  if (!/^[0-9A-Za-z_-]*$/.test(clientId)) add({ field: field.clientId, rule: 'clientId' })
  else if (clientId.length > mqttLimits.clientId)
    add({ field: field.clientId, rule: 'tooLong', params: { max: mqttLimits.clientId } })
  add(checkPrefix(field.topicPrefix, topicPrefix))
  add(checkPrefix(field.discoveryPrefix, discoveryPrefix))

  if (issues.length) return { issues, body: null }
  return {
    issues,
    body: {
      url,
      username: d.username,
      password,
      client_id: clientId,
      topic_prefix: topicPrefix,
      discovery: d.discovery,
      discovery_prefix: discoveryPrefix,
      publish_location: d.publishLocation,
    },
  }
}
