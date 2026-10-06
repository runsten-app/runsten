import { api, unwrap } from '@/shared/api'
import type { MqttBrokerUpdate, MqttSettings } from '../model/Mqtt'

export function fetchMqtt(): Promise<MqttSettings> {
  return unwrap(api.GET('/mqtt'))
}

export function setMqttBroker(broker: MqttBrokerUpdate): Promise<MqttSettings> {
  return unwrap(api.PUT('/mqtt', { body: broker }))
}

export async function deleteMqttBroker(): Promise<void> {
  await unwrap(api.DELETE('/mqtt'))
}
