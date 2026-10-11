import { describe, expect, it } from 'vitest'
import { i18n } from '@/shared/i18n'
import { ImportedBadge } from '@/shared/ui'
import { mountWith } from '@test/utils'

describe('ImportedBadge', () => {
  it.each([
    ['aximote', 'Imported from Aximote'],
    ['volvo_csv', 'Imported from the Volvo Cars app'],
    ['carbook', 'Imported from carbook'],
  ])('names the app of %s', (source, text) => {
    const { wrapper } = mountWith(ImportedBadge, { props: { source } })
    expect(wrapper.find('.imported').text()).toBe(text)
  })

  it('speaks the reader’s language', () => {
    i18n.global.locale.value = 'fr'
    try {
      const { wrapper } = mountWith(ImportedBadge, { props: { source: 'aximote' } })
      expect(wrapper.find('.imported').text()).toBe("Importé d'Aximote")
    } finally {
      i18n.global.locale.value = 'en'
    }
  })
})
