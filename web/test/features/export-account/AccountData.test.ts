import { describe, expect, it } from 'vitest'
import { h } from 'vue'
import { AccountData } from '@/features/export-account'
import { mountWith } from '@test/utils'

describe('AccountData', () => {
  it('downloads the export through a link, saved by the browser', () => {
    const { wrapper } = mountWith(AccountData)
    const link = wrapper.find('a.export')
    expect(link.attributes('href')).toBe('api/v1/account/export')
    expect(link.attributes()).toHaveProperty('download')
    expect(link.text()).toBe('Download my data')
  })

  it('shows what the slot adds after the download', () => {
    const { wrapper } = mountWith({
      render: () => h(AccountData, null, () => h('button', { class: 'extension-action' }, 'More')),
    })
    const row = wrapper.find('a.export').element.parentElement
    expect(row?.lastElementChild?.className).toBe('extension-action')
  })
})
