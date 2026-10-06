export {
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
  type Collection,
  type CollectionFailure,
  type CollectionState,
  type FailureKind,
} from './model/collection'
export { detailsStaleAfter, isStale, staleAfter } from './model/freshness'
export { vehicleLabel, vehicleLabels } from './model/label'
export { vehicleMode, type Mode } from './model/mode'
export { vehicleKeys } from './model/queryKeys'
export type {
  Cable,
  ChargingStatus,
  Connection,
  Engine,
  Model,
  ModelChoice,
  Reading,
  SourceLevel,
  State,
  VariantOption,
  Vehicle,
} from './model/Vehicle'
export { default as ReadingItem } from './ui/ReadingItem.vue'
export { default as ReadingProvenance } from './ui/ReadingProvenance.vue'
