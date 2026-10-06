// jsdom has no ResizeObserver, which Vuetify's layout uses.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// Nor visualViewport, which places Vuetify's menus.
globalThis.visualViewport ??= Object.assign(new EventTarget(), {
  width: 1024,
  height: 768,
  offsetLeft: 0,
  offsetTop: 0,
  pageLeft: 0,
  pageTop: 0,
  scale: 1,
  onresize: null,
  onscroll: null,
  onscrollend: null,
}) as VisualViewport

// Dates and numbers are those of a British reader in UTC, whatever the machine: English
// takes the browser's region (en-GB: 24-hour times), French and Swedish their own.
process.env.TZ = 'UTC'
Object.defineProperty(navigator, 'languages', { value: ['en-GB'], configurable: true })
