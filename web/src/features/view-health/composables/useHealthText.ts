import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CheckName, ServiceState, TyreWarning, Wheel } from '@/entities/health'

// useHealthText names what the health tells, each label written out so that the i18n
// lint sees every key in use.
export function useHealthText() {
  const { t } = useI18n()
  const wheels = computed<Record<Wheel, string>>(() => ({
    front_left: t('health.tyres.wheel.front_left'),
    front_right: t('health.tyres.wheel.front_right'),
    rear_left: t('health.tyres.wheel.rear_left'),
    rear_right: t('health.tyres.wheel.rear_right'),
  }))
  const tyreWarnings = computed<Record<TyreWarning, string>>(() => ({
    ok: t('health.tyres.warning.ok'),
    low: t('health.tyres.warning.low'),
    very_low: t('health.tyres.warning.very_low'),
    high: t('health.tyres.warning.high'),
    fault: t('health.tyres.warning.fault'),
  }))
  const serviceStates = computed<Record<ServiceState, string>>(() => ({
    ok: t('health.service.state.ok'),
    soon: t('health.service.state.soon'),
    due: t('health.service.state.due'),
    overdue: t('health.service.state.overdue'),
  }))
  const checks = computed<Record<CheckName, string>>(() => ({
    brake_fluid: t('health.check.brake_fluid'),
    coolant: t('health.check.coolant'),
    oil: t('health.check.oil'),
    washer_fluid: t('health.check.washer_fluid'),
    low_voltage_battery: t('health.check.low_voltage_battery'),
    light_brake_center: t('health.check.light_brake_center'),
    light_brake_left: t('health.check.light_brake_left'),
    light_brake_right: t('health.check.light_brake_right'),
    light_daytime_left: t('health.check.light_daytime_left'),
    light_daytime_right: t('health.check.light_daytime_right'),
    light_fog_front: t('health.check.light_fog_front'),
    light_fog_rear: t('health.check.light_fog_rear'),
    light_hazard: t('health.check.light_hazard'),
    light_high_beam_left: t('health.check.light_high_beam_left'),
    light_high_beam_right: t('health.check.light_high_beam_right'),
    light_low_beam_left: t('health.check.light_low_beam_left'),
    light_low_beam_right: t('health.check.light_low_beam_right'),
    light_position_front_left: t('health.check.light_position_front_left'),
    light_position_front_right: t('health.check.light_position_front_right'),
    light_position_rear: t('health.check.light_position_rear'),
    light_position_rear_left: t('health.check.light_position_rear_left'),
    light_position_rear_right: t('health.check.light_position_rear_right'),
    light_registration_plate: t('health.check.light_registration_plate'),
    light_reverse: t('health.check.light_reverse'),
    light_side_mark: t('health.check.light_side_mark'),
    light_turn_front_left: t('health.check.light_turn_front_left'),
    light_turn_front_right: t('health.check.light_turn_front_right'),
    light_turn_rear_left: t('health.check.light_turn_rear_left'),
    light_turn_rear_right: t('health.check.light_turn_rear_right'),
  }))
  return { wheels, tyreWarnings, serviceStates, checks }
}
