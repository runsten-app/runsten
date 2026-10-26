import type { Component } from 'vue'

// extensionSections adds none: the public build's settings are complete on their own. The
// hosted offer's build replaces this file with its own sections, shown in order after
// the access tokens and before the account's data, each with its heading.
export const extensionSections: Component[] = []

// extensionAccountActions adds none beside the download of the account's data. The hosted
// offer's build lists its own there, each a button, shown in order after the download.
export const extensionAccountActions: Component[] = []

// releaseSection ends the settings with the release the instance runs, for whoever runs
// it: a build whose users do not update it sets it false.
export const releaseSection = true
