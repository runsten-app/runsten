// Intl.DurationFormat is in every browser Runsten targets and in Node 24, but not yet in
// the lib of TypeScript 5.9: the part Runsten uses.
declare namespace Intl {
  interface DurationFormatOptions {
    style?: 'long' | 'short' | 'narrow' | 'digital'
  }
  interface DurationInput {
    hours?: number
    minutes?: number
  }
  class DurationFormat {
    constructor(locale?: string | string[], options?: DurationFormatOptions)
    format(duration: DurationInput): string
  }
}
