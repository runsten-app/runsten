// Apart from the rest of shared/ui: Chart.js comes with it, and only what the router or
// a page loads on demand imports it (the statistics and battery pages, the charts of the
// vehicle and charge pages).
export { default as ChartFrame } from './ChartFrame.vue'
export type { Band, Point, Series } from './series'
