import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import {
  deleteMqttBroker,
  fetchMqtt,
  mqttKeys,
  setMqttBroker,
  type MqttBrokerUpdate,
} from '@/entities/mqtt'

// statusEvery is how often the collector writes what it knows of its connection: the
// broker is read again as often, for its status.
const statusEvery = 60_000

// useMqtt is the account's MQTT broker, and the collector's connection to it.
export function useMqtt() {
  const { data, isPending, error } = useQuery({
    queryKey: mqttKeys.current(),
    queryFn: fetchMqtt,
    refetchInterval: statusEvery,
  })
  return { mqtt: data, isPending, error }
}

// useSetMqttBroker sets the broker; the collector connects to it at its next pass.
export function useSetMqttBroker() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (broker: MqttBrokerUpdate) => setMqttBroker(broker),
    onSuccess: (mqtt) => queryClient.setQueryData(mqttKeys.current(), mqtt),
  })
  return { setMqttBroker: mutateAsync, isPending, error, reset }
}

// useDeleteMqttBroker removes it: nothing is published any more.
export function useDeleteMqttBroker() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deleteMqttBroker,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: mqttKeys.current() }),
  })
  return { deleteMqttBroker: mutateAsync, isPending, error, reset }
}
