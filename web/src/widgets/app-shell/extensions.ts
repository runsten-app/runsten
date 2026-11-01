import type { Component, Ref } from 'vue'

// extensionNotices shows none: the public build has nothing to tell above every page. The
// hosted offer's build replaces this file with its own, shown in order at the top of
// every page, above the page's own, each deciding whether it has something to say.
export const extensionNotices: Component[] = []

// extensionMenuItems adds none to the user menu. The hosted offer's build lists its own
// there, each a v-list-item, shown in order after the settings and the connection.
export const extensionMenuItems: Component[] = []

// extensionAttention asks nothing of the user. The hosted offer's build lists its own,
// each a composable the shell calls once: a sentence puts a dot on the user menu, read
// with its name, after the connection's; undefined, none. The item it concerns puts its own.
export const extensionAttention: (() => Readonly<Ref<string | undefined>>)[] = []
