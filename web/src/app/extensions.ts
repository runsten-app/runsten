// beforeStart prepares nothing: the public build starts the app as it is. A downstream
// build replaces this file to prepare the page first, such as replacing globalThis.fetch,
// which every API call looks up at each call; main awaits it before the router's first
// navigation and the app's mount.
export async function beforeStart(): Promise<void> {}
