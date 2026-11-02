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
  // wide: a line drawn wider and under the others, so that another one of the same values
  // still shows, within it (two tyres of one pressure).
  wide?: boolean
  // band: filled towards the previous series, its lower bound (an interquartile range),
  // which is an edge: both are thin lines, the fill in the band color.
  band?: boolean
  edge?: boolean
  // soft: bars in the band color, edged in the series' own (the uncertainty of a cost).
  soft?: boolean
  // How the table for screen readers and the tooltip show this series' figures, when
  // they are not the chart's values (a count, another unit).
  format?: (v: number | null) => string
  // area: the line filled down to the axis, faintly (a level read over time).
  area?: boolean
  // right: drawn against the chart's second axis, on the right (a state of charge beside
  // a power).
  right?: boolean
}

// Band is a span of the x axis drawn in a strip under the plot, named inside where it
// has room (a trip, a charge, parked): faint where it may have been, solid where it
// surely was. Later bands are drawn over earlier ones. Bands of one label share their
// color and their entry in the legend.
export type Band = {
  label: string
  color: string
  // The color of its name, over the band's; the theme's on-primary without it.
  ink?: string
  // What the pointer over it tells under its name, a line each (its day, times, duration).
  detail?: string[]
  from: number
  to: number
  // The part it surely covers; none when nothing is sure (an event never seen).
  sure?: { from: number; to: number }
}
