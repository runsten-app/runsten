import { describe, expect, it } from 'vitest'
import { h } from 'vue'
import { ErrorSummary } from '@/shared/ui'
import { mountWith } from '@test/utils'

describe('ErrorSummary', () => {
  it('lists the errors, each leading to its field without leaving the page', async () => {
    const Host = {
      render: () => [
        h(ErrorSummary, {
          title: 'Fix these',
          items: [{ field: 'far', text: 'Far: required.' }, { text: 'Refused.' }],
        }),
        h('details', [h('summary', 'More'), h('input', { id: 'far' })]),
      ],
    }
    const { wrapper } = mountWith(Host, { attachTo: document.body })
    expect(wrapper.find('h2').text()).toBe('Fix these')
    const items = wrapper.findAll('li')
    expect(items.map((li) => li.text())).toEqual(['Far: required.', 'Refused.'])
    expect(items[1]?.find('a').exists()).toBe(false)
    const link = items[0]?.find('a')
    expect(link?.attributes('href')).toBe('#far')
    await link?.trigger('click')
    // A field inside a closed <details> opens it.
    expect(wrapper.find('details').element.open).toBe(true)
    expect(document.activeElement?.id).toBe('far')
    wrapper.unmount()
  })

  it('takes the focus when asked', () => {
    const refs: { summary?: { focus: () => void } } = {}
    const Host = {
      render: () =>
        h(ErrorSummary, {
          ref: (r: unknown) => (refs.summary = r as typeof refs.summary),
          title: 'Fix these',
          items: [{ text: 'Refused.' }],
        }),
    }
    const { wrapper } = mountWith(Host, { attachTo: document.body })
    refs.summary?.focus()
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
    wrapper.unmount()
  })
})
