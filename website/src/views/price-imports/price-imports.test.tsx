import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import routes from '@/router'
import type { PriceImportPreview } from '@/types/price-imports'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let permissions: string[],
  requests: InternalAxiosRequestConfig[],
  result: PriceImportPreview,
  failure: number,
  hold: Promise<void> | undefined
let downloaded: { name: string; url: string } | undefined
const original = client.defaults.adapter
const csv =
  'provider_model_id,metric,tier,unit,currency,amount,enabled,context_threshold\npmo_one,INPUT_TOKEN,base,1M_TOKEN,USD,0,false,0\n'
const api = '/admin/prices/import/'
beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  permissions = ['prices.read', 'prices.write']
  requests = []
  failure = 0
  hold = undefined
  downloaded = undefined
  const rate = {
    metric: 'INPUT_TOKEN' as const,
    tier: 'base' as const,
    unit: '1M_TOKEN' as const,
    currency: 'USD' as const,
    amount: '0',
    enabled: false,
  }
  result = {
    etag: 'catalogue-one',
    preview_digest: 'digest-one',
    valid: true,
    items: [],
    errors: [],
    changes: [
      {
        row: 2,
        provider_model_id: 'pmo_one',
        upstream_name: 'Authoritative model',
        action: 'updated',
        before: { ...rate, amount: '1.234567890123456789', enabled: true },
        after: rate,
        threshold_before: 128000,
        threshold_after: 0,
        stops_following: true,
      },
    ],
  }
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = vi.fn(() => 'blob:fixture')
      static revokeObjectURL = vi.fn()
    },
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloaded = { name: this.download, url: this.href }
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session')
      response.data = {
        user: {
          id: 'usr_import',
          name: 'Reviewer',
          email: 'reviewer@example.test',
          role: 'member',
        },
        csrf_token: 'csrf-import',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === api + 'preview') response.data = structuredClone(result)
    else if (config.url === api + 'commit') {
      if (hold) await hold
      if (failure) {
        response.status = failure
        response.data = { preview: result }
        throw new AxiosError('Fixture', '', config, undefined, response)
      }
      response.data = { preview: result, catalogue: { etag: 'saved', items: [] } }
    } else if (config.url === '/admin/prices/export.csv')
      response.data = new Blob([csv], { type: 'text/csv' })
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  client.defaults.adapter = original
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function until(check: () => void) {
  for (let i = 0; i < 80; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 79) throw error
    }
  }
}
async function mount() {
  router = createMemoryRouter(routes, { initialEntries: ['/admin/prices'] })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      permissions.includes('prices.read') ? 'Upload prices' : 'Access denied',
    ),
  )
}
function button(label: string) {
  const node = [...document.querySelectorAll('button')].find((node) => node.textContent === label)
  expect(node, label).toBeDefined()
  return node!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function select(file: File) {
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')!
  await act(async () => {
    Object.defineProperty(input, 'files', { configurable: true, value: [file] })
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await until(() => expect(input.disabled).toBe(false))
}
async function preview() {
  await select(new File([csv], 'edited.csv'))
  await click('Validate file')
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Authoritative model'),
  )
}
const commits = () => requests.filter((r) => r.url === api + 'commit')
describe('CSV price maintenance', () => {
  it('guards access and gives readers preview/export without commit or initial writes', async () => {
    permissions = []
    await mount()
    expect(requests.some((r) => r.url?.startsWith('/admin/prices'))).toBe(false)
    permissions = ['prices.read']
    await act(async () => cache.setQueryData(['permissions', 'usr_import'], permissions))
    await until(() => expect(host.querySelector('input[type="file"]')).not.toBeNull())
    expect(requests.some((r) => r.method === 'post')).toBe(false)
    await preview()
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain(
      'Applying changes requires',
    )
    expect(document.body.textContent).not.toContain('Confirm import')
    expect(commits()).toHaveLength(0)
  })
  it('rejects unsupported, oversized, and malformed UTF-8 files before preview', async () => {
    await mount()
    for (const [file, message] of [
      [new File(['x'], 'prices.xlsx'), 'Choose a CSV'],
      [new File(['x'.repeat(32769)], 'large.csv'), 'exceeds 32 KiB'],
      [new File([new Uint8Array([0xff])], 'encoding.csv'), 'valid UTF-8'],
    ] as const) {
      await select(file)
      expect(host.textContent).toContain(message)
      expect(button('Validate file').disabled).toBe(true)
    }
    expect(requests.some((r) => r.url === api + 'preview')).toBe(false)
  })
  it('shows all row and file errors and never partially applies invalid previews', async () => {
    result = {
      ...result,
      valid: false,
      preview_digest: '',
      errors: [
        { row: 0, column: '', code: 'model_limit', message: 'At most 20 models' },
        { row: 162, column: '', code: 'row_limit', message: 'At most 160 rows' },
        { row: 3, column: 'amount', code: 'invalid_decimal', message: 'Invalid amount' },
        { row: 4, column: 'provider_model_id', code: 'unknown_model', message: 'Unknown identity' },
      ],
    }
    await mount()
    await preview()
    const dialog = document.querySelector('[role="dialog"]')!
    for (const error of result.errors) expect(dialog.textContent).toContain(error.message)
    expect(dialog.textContent).toContain('Row 162')
    expect(button('Confirm import').disabled).toBe(true)
    expect(commits()).toHaveLength(0)
  })
  it('shows authoritative identity and exact old/new states, then commits only captured CSV/ETag/digest with CSRF once', async () => {
    await mount()
    await preview()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('pmo_one')
    expect(dialog.textContent).toContain('1.234567890123456789 USD')
    expect(dialog.textContent).toContain('0 USD')
    expect(dialog.textContent).toContain('Disabled')
    expect(dialog.textContent).toContain('128000 → 0')
    expect(dialog.textContent).toContain('Stops following repository')
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    const confirm = button('Confirm import')
    await act(async () => {
      confirm.click()
      confirm.click()
    })
    await until(() => expect(commits()).toHaveLength(1))
    expect(button('Working…').disabled).toBe(true)
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('CSV price changes were applied.'))
    expect(JSON.parse(commits()[0].data)).toEqual({
      csv,
      etag: 'catalogue-one',
      preview_digest: 'digest-one',
    })
    expect(commits()[0].headers.get('X-CSRF-Token')).toBe('csrf-import')
    expect(requests.find((r) => r.url === api + 'preview')!.headers.get('X-CSRF-Token')).toBe(
      'csrf-import',
    )
    expect(localStorage.length).toBe(0)
  })
  it('requires repreview after conflict and preserves the exact selected file before a new confirmation', async () => {
    await mount()
    await preview()
    failure = 409
    await click('Confirm import')
    await until(() => expect(document.body.textContent).toContain('The catalogue changed.'))
    expect(button('Confirm import').disabled).toBe(true)
    result = { ...result, etag: 'catalogue-new', preview_digest: 'digest-new' }
    failure = 0
    await click('Reload catalogue and preview again')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
        'Authoritative model',
      ),
    )
    expect(commits()).toHaveLength(1)
    await click('Confirm import')
    await until(() => expect(commits()).toHaveLength(2))
    expect(JSON.parse(commits()[1].data)).toEqual({
      csv,
      etag: 'catalogue-new',
      preview_digest: 'digest-new',
    })
  })
  it('reports 422 located errors and uncertain publication without claiming success', async () => {
    await mount()
    await preview()
    failure = 422
    result = {
      ...result,
      valid: false,
      preview_digest: '',
      errors: [
        { row: 2, column: 'currency', code: 'missing_fx', message: 'Conversion unavailable' },
      ],
    }
    await click('Confirm import')
    await until(() => expect(document.body.textContent).toContain('Conversion unavailable'))
    expect(button('Confirm import').disabled).toBe(true)
    await click('Cancel')
    result = { ...result, valid: true, preview_digest: 'new', errors: [] }
    failure = 503
    await click('Validate file')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
        'Authoritative model',
      ),
    )
    await click('Confirm import')
    await until(() => expect(document.body.textContent).toContain('catalogue may have been saved'))
    expect(document.body.textContent).not.toContain('CSV price changes were applied.')
    expect(button('Confirm import').disabled).toBe(true)
  })
  it('replaces file intent only after a new preview and keeps labels reactive while preserving canonical data', async () => {
    await mount()
    await preview()
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('价格差异预览')
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Authoritative model')
    await click('取消')
    await act(async () => i18n.changeLanguage('en'))
    const edited = csv.replace(',0,false,', ',4,true,')
    await select(new File([edited], 'replacement.csv'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await click('Validate file')
    await until(() => expect(requests.filter((r) => r.url === api + 'preview')).toHaveLength(2))
    expect(JSON.parse(requests.filter((r) => r.url === api + 'preview')[1].data)).toEqual({
      csv: edited,
    })
    expect(commits()).toHaveLength(0)
  })
  it('downloads an authenticated CSV blob with a safe fixed filename and releases its object URL', async () => {
    await mount()
    await click('Download CSV')
    await until(() =>
      expect(downloaded).toEqual({ name: 'routex-prices.csv', url: 'blob:fixture' }),
    )
    expect(requests.find((r) => r.url === '/admin/prices/export.csv')!.responseType).toBe('blob')
    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    await until(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:fixture'))
    expect(host.textContent).toContain('CSV download was prepared.')
    expect(commits()).toHaveLength(0)
  })
})
