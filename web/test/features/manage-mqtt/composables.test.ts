import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mqttKeys } from '@/entities/mqtt'
import { useDeleteMqttBroker, useMqtt, useSetMqttBroker } from '@/features/manage-mqtt'
import mqtt from '@fixtures/mqtt.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchMqtt: vi.fn(),
  setMqttBroker: vi.fn(),
  deleteMqttBroker: vi.fn(),
}))
vi.mock('@/entities/mqtt', async (original) => ({
  ...(await original<typeof import('@/entities/mqtt')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

const body = {
  url: 'mqtt://homeassistant.local:1883',
  username: '',
  password: null,
  client_id: '',
  topic_prefix: 'runsten',
  discovery: true,
  discovery_prefix: 'homeassistant',
  publish_location: true,
}

describe('useMqtt', () => {
  it('reads the broker', async () => {
    api.fetchMqtt.mockResolvedValue(mqtt)
    const { result } = withSetup(useMqtt)
    await flushPromises()
    expect(result.mqtt.value).toEqual(mqtt)
  })
})

describe('useSetMqttBroker', () => {
  it('sets the broker and keeps what the API gives back', async () => {
    api.setMqttBroker.mockResolvedValue(mqtt)
    const { result, queryClient } = withSetup(useSetMqttBroker)
    expect(await result.setMqttBroker(body)).toEqual(mqtt)
    expect(api.setMqttBroker).toHaveBeenCalledWith(body)
    expect(queryClient.getQueryData(mqttKeys.current())).toEqual(mqtt)
  })
})

describe('useDeleteMqttBroker', () => {
  it('removes the broker and reads it again', async () => {
    api.deleteMqttBroker.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeleteMqttBroker)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteMqttBroker()
    expect(api.deleteMqttBroker).toHaveBeenCalled()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: mqttKeys.current() })
  })
})
