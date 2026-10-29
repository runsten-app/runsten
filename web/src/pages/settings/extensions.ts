import type { Component } from 'vue'

// extensionSections adds none: the public build's settings are complete on their own. The
// hosted offer's build replaces this file with its own sections, shown in order in the
// integrations' area, after the access tokens and MQTT, each a section with its heading
// (an h3, under the area's h2).
export const extensionSections: Component[] = []

// extensionAccountSections adds none either: sections of the account's area, shown in
// order before the account's data, each a section with its heading (an h3).
export const extensionAccountSections: Component[] = []

// extensionAccountActions adds none beside the download of the account's data. The hosted
// offer's build lists its own there, each a button, shown in order after the download.
export const extensionAccountActions: Component[] = []

// releaseSection ends the account's area with the release the instance runs, for whoever
// runs it: a build whose users do not update it sets it false.
export const releaseSection = true
