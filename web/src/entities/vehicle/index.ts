export {
  deleteVehicle,
  fetchVariants,
  fetchVehicle,
  fetchVehicles,
  fetchVehicleState,
  setVehicleModel,
} from './api/vehicleApi'
export {
  collectionState,
  collectorStoppedAfter,
  needsAttention,
  notGrantedFor,
  type Collection,
  type CollectionFailure,
  type CollectionState,
  type FailureKind,
} from './model/collection'
export {
  detailsStaleAfter,
  isStale,
  lastReport,
  reportLateAfter,
  staleAfter,
} from './model/freshness'
export { vehicleLabel, vehicleLabels } from './model/label'
export { vehicleMode, type Mode } from './model/mode'
export { vehicleKeys } from './model/queryKeys'
export type {
  Cable,
  ChargingStatus,
  CurrentChargeType,
  Connection,
  Engine,
  Model,
  ModelChoice,
  Reading,
  Sleep,
  SoftwareUpdate,
  UpdateStatus,
  SourceLevel,
  State,
  VariantOption,
  Vehicle,
} from './model/Vehicle'
export { default as ReadingItem } from './ui/ReadingItem.vue'
export { default as ReadingProvenance } from './ui/ReadingProvenance.vue'
