import { describe, expect, it } from 'vitest'
import type { MqttBroker } from '@/entities/mqtt'
import { draftOf, hostOf, mqttLimits, validate } from '@/features/manage-mqtt'
import mqtt from '@fixtures/mqtt.json'

const stored = mqtt.broker as MqttBroker
const draft = (more: Partial<ReturnType<typeof draftOf>> = {}) => ({
  ...draftOf(null),
  url: 'mqtt://homeassistant.local:1883',
  ...more,
})

describe('validate', () => {
  it('gives the body of a new broker, with the defaults, trimmed', () => {
    const { issues, body } = validate(
      draft({ url: ' mqtt://homeassistant.local:1883 ', username: 'u', password: 'p' }),
      null,
      false,
    )
    expect(issues).toEqual([])
    expect(body).toEqual({
      url: 'mqtt://homeassistant.local:1883',
      username: 'u',
      password: 'p',
      client_id: '',
      topic_prefix: 'runsten',
      discovery: true,
      discovery_prefix: 'homeassistant',
      publish_location: true,
    })
  })

  it.each([
    ['', 'required'],
    ['homeassistant.local', 'scheme'],
    ['http://broker.example', 'scheme'],
    ['mqtt://', 'host'],
    ['mqtt:broker.example', 'host'],
    ['mqtt://user:secret@broker.example', 'credentials'],
    ['mqtt://broker.example/topic', 'path'],
    ['mqtt://broker.example?x=1', 'path'],
    ['mqtt://broker.example:0', 'port'],
    ['mqtt://broker.example:65536', 'port'],
    [`mqtt://${'a'.repeat(mqttLimits.url)}`, 'tooLong'],
  ])('refuses the URL %j: %s', (url, rule) => {
    const { issues, body } = validate(draft({ url }), null, false)
    expect(body).toBeNull()
    expect(issues.map((i) => [i.field, i.rule])).toEqual([['mqtt-url', rule]])
  })

  it('accepts the ports, IPv6 and a trailing slash', () => {
    for (const url of ['mqtts://broker.example:8883', 'mqtt://[fd00::1]:1883', 'MQTT://Host/']) {
      expect(validate(draft({ url }), null, false).issues).toEqual([])
    }
  })

  it('wants TLS where the instance publishes to the Internet only', () => {
    expect(validate(draft(), null, true).issues.map((i) => i.rule)).toEqual(['tls'])
    expect(validate(draft({ url: 'mqtts://broker.example' }), null, true).issues).toEqual([])
  })

  it.each([
    ['topicPrefix', 'runsten/+', 'mqtt-topic-prefix', 'prefix'],
    ['topicPrefix', '/runsten', 'mqtt-topic-prefix', 'prefix'],
    ['topicPrefix', 'a//b', 'mqtt-topic-prefix', 'prefix'],
    ['topicPrefix', '  ', 'mqtt-topic-prefix', 'required'],
    ['discoveryPrefix', '#', 'mqtt-discovery-prefix', 'prefix'],
    ['discoveryPrefix', 'a'.repeat(mqttLimits.prefix + 1), 'mqtt-discovery-prefix', 'tooLong'],
    ['clientId', 'car one', 'mqtt-client-id', 'clientId'],
    ['clientId', 'a'.repeat(mqttLimits.clientId + 1), 'mqtt-client-id', 'tooLong'],
    ['username', 'u'.repeat(mqttLimits.username + 1), 'mqtt-username', 'tooLong'],
    ['password', 'p'.repeat(mqttLimits.password + 1), 'mqtt-password', 'tooLong'],
  ])('refuses %s %j', (key, value, field, rule) => {
    const { issues } = validate(draft({ [key]: value }), null, false)
    expect(issues.map((i) => [i.field, i.rule])).toEqual([[field, rule]])
  })

  it('keeps the stored password for the same host only, or removes it', () => {
    const keep = (url: string) => validate(draft({ url }), stored, false)
    expect(keep('mqtts://HomeAssistant.local:8883').body?.password).toBeNull()
    expect(keep('mqtt://elsewhere.example').issues.map((i) => [i.field, i.rule])).toEqual([
      ['mqtt-password', 'passwordAgain'],
    ])
    // The URL's own issue first: the host is not known.
    expect(keep('mqtt://').issues.map((i) => i.rule)).toEqual(['host'])
    const other = { url: 'mqtt://elsewhere.example' }
    expect(validate(draft({ ...other, password: 'new' }), stored, false).body?.password).toBe('new')
    expect(validate(draft({ ...other, removePassword: true }), stored, false).body?.password).toBe(
      '',
    )
    expect(validate(draft(other), null, false).body?.password).toBe('')
  })

  it('fills the form from a broker, never with its password', () => {
    expect(draftOf(stored)).toEqual({
      url: stored.url,
      username: stored.username,
      password: '',
      removePassword: false,
      clientId: '',
      topicPrefix: 'runsten',
      discovery: true,
      discoveryPrefix: 'homeassistant',
      publishLocation: true,
    })
  })

  it('compares hosts as the API does', () => {
    expect(hostOf('mqtt://HomeAssistant.Local:1883')).toBe('homeassistant.local')
    expect(hostOf('mqtts://[FD00::1]')).toBe('fd00::1')
    expect(hostOf('mqtt://')).toBeNull()
    expect(hostOf('nothing')).toBeNull()
  })
})
