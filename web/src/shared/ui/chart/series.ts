// A point of a series over a linear x axis; y is null where the value is unknown.
export type Point = { x: number; y: number | null }

// Series is one set of values of a chart, an interval each; null is unknown, a hole
// never a zero. A series over a linear axis gives its points as {x, y}.
export type Series = {
  label: string
  data: (number | null)[] | Point[]
  color: string
  // A scatter draws points, not a line (the estimates behind each point).
  scatter?: boolean
  // dashed: a reference to compare against, not data of its own.
  dashed?: boolean
  // band: filled towards the previous series, its lower bound (an interquartile range),
  // which is an edge: both are thin lines, the fill in the band color.
  band?: boolean
  edge?: boolean
  // soft: bars in the band color, edged in the series' own (the uncertainty of a cost).
  soft?: boolean
  // How the table for screen readers and the tooltip show this series' figures, when
  // they are not the chart's values (a count, another unit).
  format?: (v: number | null) => string
  // right: drawn against the chart's second axis, on the right (a state of charge beside
  // a power).
  right?: boolean
}

// Band is a span of the x axis drawn behind the series, full height (a trip, a charge):
// faint where it may have been, stronger where it surely was. Bands of one label share
// their color and their entry in the legend.
export type Band = {
  label: string
  color: string
  from: number
  to: number
  // The part it surely covers; none when nothing is sure (an event never seen).
  sure?: { from: number; to: number }
}
