import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tripKeys } from '@/entities/trip'
import { importKeys, type FileImport } from '@/entities/vehicle-import'
import { ImportSources } from '@/features/manage-imports'
import { ApiError } from '@/shared/api'
import reportFixture from '@fixtures/import-aaos-logging.json'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchImports: vi.fn(),
  fetchImportJob: vi.fn(),
  importAAOSLogging: vi.fn(),
}))
vi.mock('@/entities/vehicle-import', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle-import')>()),
  ...api,
}))

const report = reportFixture as FileImport
const exported = new File(['PK'], 'AAOSLogging.zip', { type: 'application/zip' })
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.fetchImports.mockResolvedValue([])
  api.fetchImportJob.mockResolvedValue(null)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted() {
  const queryClient = testQueryClient()
  const m = mountWith(ImportSources, {
    queryClient,
    props: { vehicle: 'v1', volvo: false },
    attachTo: document.body,
  })
  wrapper = m.wrapper
  await flushPromises()
  await m.wrapper.find('.aaos-import .toggle').trigger('click')
  await flushPromises()
  return { w: m.wrapper, queryClient }
}

// choose sets the file of the field, as the browser's picker does.
async function choose(w: VueWrapper, file: File) {
  const input = w.find('.aaos-import .export input[type="file"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
}

async function submit(w: VueWrapper) {
  await w.find('.aaos-form').trigger('submit')
  await flushPromises()
}

const status = (w: VueWrapper) => w.find('.aaos-import [role="status"]').text()
const summary = (w: VueWrapper) =>
  w.findAll('.aaos-import .error-summary li').map((li) => li.text())

describe('AAOSLoggingImport', () => {
  it('waits behind a button, which opens the form on its field and gets the focus back', async () => {
    const { w } = await mounted()
    expect(document.activeElement?.id).toBe('aaos-export')
    expect(w.find('.aaos-import input[type="file"]').attributes('accept')).toBe(
      '.zip,application/zip',
    )
    await w.find('.aaos-form .cancel').trigger('click')
    await flushPromises()
    expect(w.find('.aaos-form').exists()).toBe(false)
    const toggle = w.find('.aaos-import .toggle')
    expect(toggle.text()).toBe('Choose the export')
    expect(document.activeElement).toBe(toggle.element)
  })

  it('imports the export, shows the report, and reads again what changed', async () => {
    const { w, queryClient } = await mounted()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(w.find('.aaos-import a').attributes()).toMatchObject({
      href: 'https://play.google.com/store/apps/details?id=com.aaoslogging',
      target: '_blank',
    })
    await choose(w, exported)
    api.importAAOSLogging.mockResolvedValue(report)
    await submit(w)
    expect(api.importAAOSLogging).toHaveBeenCalledWith('v1', exported)
    expect(status(w)).toBe('Import finished')
    expect(w.find('.aaos-form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('aaos-import-toggle')
    const lines = w.findAll('.aaos-import .import-report p').map((p) => p.text())
    expect(lines).toEqual([
      '3 trips imported.',
      "One left: it starts after the vehicle's first reading (Monday, 28 September 2026).",
      'One trip left, unfinished or out of bounds.',
    ])
    const keys = invalidate.mock.calls.map(
      ([f]) => (f as { queryKey?: unknown } | undefined)?.queryKey,
    )
    expect(keys).toContainEqual(importKeys.list('v1'))
    expect(keys).toContainEqual(tripKeys.lists('v1'))
  })

  it('keeps the form while the export is sent', async () => {
    const { w } = await mounted()
    await choose(w, exported)
    let finish: (r: FileImport) => void = () => {}
    api.importAAOSLogging.mockReturnValue(new Promise<FileImport>((r) => (finish = r)))
    await w.find('.aaos-form').trigger('submit')
    await flushPromises()
    expect(w.find('.aaos-form .cancel').attributes('disabled')).toBeDefined()
    finish(report)
    await flushPromises()
    expect(w.find('.aaos-form').exists()).toBe(false)
  })

  it('asks for the export first', async () => {
    const { w } = await mounted()
    await submit(w)
    expect(api.importAAOSLogging).not.toHaveBeenCalled()
    expect(summary(w)).toEqual(["Choose the ZIP of the car's folder."])
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
  })

  it.each([
    ['invalid_file', 400, "This is not AAOS Logging's export, or it is incomplete"],
    ['invalid_body', 400, 'The export is too large'],
    ['network', 0, 'Runsten could not be reached'],
    ['internal', 500, 'The export could not be imported'],
  ] as const)('tells why the export was refused: %s', async (code, httpStatus, text) => {
    const { w } = await mounted()
    await choose(w, exported)
    api.importAAOSLogging.mockRejectedValue(new ApiError(httpStatus, code, code))
    await submit(w)
    expect(summary(w)[0]).toContain(text)
    expect(w.find('.aaos-import .export').text()).toContain(text)
    expect(w.find('.aaos-import .import-report').exists()).toBe(false)
    // Another file clears the error.
    await choose(w, new File(['x'], 'other.zip'))
    await flushPromises()
    expect(summary(w)).toEqual([])
  })
})
