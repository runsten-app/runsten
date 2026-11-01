export {
  deleteVehicle,
  fetchVariants,
  fetchVehicle,
  fetchVehicles,
  fetchVehicleState,
  readingsCsvPath,
  setVehicleModel,
  vehiclePicturePath,
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
export {
  openings,
  type Doors,
  type OpenItem,
  type Opening,
  type OpeningName,
  type Openings,
  type Windows,
} from './model/openings'
export { vehicleKeys } from './model/queryKeys'
export type {
  Cable,
  ChargingStatus,
  CurrentChargeType,
  Connection,
  Engine,
  Model,
  ModelChoice,
  Sheet,
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
export { default as OpeningsSummary } from './ui/OpeningsSummary.vue'
export { default as ReadingProvenance } from './ui/ReadingProvenance.vue'
export { default as VehiclePicture } from './ui/VehiclePicture.vue'
export { default as VehicleSheet } from './ui/VehicleSheet.vue'
