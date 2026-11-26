// leave sends the browser to Ford's sign-in page, in this tab: Ford comes back to this
// instance by itself.
export function leave(url: string) {
  window.location.assign(url)
}
