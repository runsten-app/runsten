import type { components } from '@/shared/api'

// MqttSettings is the account's MQTT broker, which the collector publishes the vehicles'
// state to, and what the collector wrote of its connection to it. The password never
// comes back from the API: whether there is one only.
export type MqttSettings = components['schemas']['MqttSettings']
export type MqttBroker = components['schemas']['MqttBroker']
export type MqttBrokerUpdate = components['schemas']['MqttBrokerUpdate']
export type MqttStatus = components['schemas']['MqttStatus']
export type MqttFailure = NonNullable<MqttStatus['failure']>
