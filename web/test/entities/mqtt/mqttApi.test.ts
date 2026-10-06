import { afterEach, describe, expect, it, vi } from 'vitest'
import { deleteMqttBroker, fetchMqtt, mqttKeys, setMqttBroker } from '@/entities/mqtt'
import mqtt from '@fixtures/mqtt.json'
import { json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const broker = {
  url: 'mqtt://homeassistant.local:1883',
  username: 'runsten',
  password: null,
  client_id: '',
  topic_prefix: 'runsten',
  discovery: true,
  discovery_prefix: 'homeassistant',
  publish_location: true,
}

describe('MQTT API', () => {
  it('reads the broker, sets it and removes it', async () => {
    const requests = stubFetch({
      'GET /mqtt': () => json(mqtt),
      'PUT /mqtt': () => json(mqtt),
      'DELETE /mqtt': () => new Response(null, { status: 204 }),
    })
    expect(await fetchMqtt()).toEqual(mqtt)
    expect(await setMqttBroker(broker)).toEqual(mqtt)
    await deleteMqttBroker()
    expect(await requests[1]?.json()).toEqual(broker)
    expect(requests.map((r) => r.method)).toEqual(['GET', 'PUT', 'DELETE'])
  })

  it('names its query', () => {
    expect(mqttKeys.current()).toEqual(['mqtt'])
  })
})
