import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/storage'
import zh from '@/i18n/locales/zh/storage'
import { sessionKey } from '@/hooks/use-auth'
import type { StorageProbe, StorageRevision, StorageSettings } from '@/types/storage'
import StoragePage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const original = client.defaults.adapter
const session = {
  user: { id: 'usr_storage', name: 'Admin', email: 'admin@example.test', role: 'admin' },
  csrf_token: 'csrf-storage',
}
const current: StorageRevision = {
  id: 'str_current',
  endpoint: 'https://s3.example.test',
  region: 'us-east-1',
  bucket: 'routex-files',
  prefix: 'attachments/',
  credentials_configured: true,
  verified_at: '2026-09-28T12:00:00Z',
  created_at: '2026-09-28T11:59:00Z',
}
const previous: StorageRevision = {
  ...current,
  id: 'str_previous',
  endpoint: 'https://archive.example.test',
  bucket: 'routex-archive',
  prefix: 'previous/',
  verified_at: '2026-09-20T12:00:00Z',
  created_at: '2026-09-20T11:59:00Z',
}
const seed: StorageSettings = {
  enabled: true,
  etag: 'revision_1',
  revision: current,
  revisions: [current, previous],
}
const probe: StorageProbe = {
  success: true,
  cleanup_pending: false,
  stages: [
    { name: 'put', status: 'passed', duration_ms: 12 },
    { name: 'get', status: 'passed', duration_ms: 8 },
    { name: 'delete', status: 'passed', duration_ms: 5 },
  ],
}

let root: Root
let host: HTMLDivElement
let cache: QueryClient
let row: StorageSettings
let permissions: string[]
let requests: InternalAxiosRequestConfig[]
let failure: number
let readFailure: number
let hold: boolean
let release: (() => void) | undefined

beforeEach(async () => {
  await i18n.changeLanguage('en')
  row = structuredClone(seed)
  permissions = ['storage.read', 'storage.write', 'storage.test']
  requests = []
  failure = 0
  readFailure = 0
  hold = false
  release = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, session)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = session
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.method === 'get' && config.url === '/admin/storage') {
      if (readFailure) {
        const code = readFailure
        readFailure = 0
        throw new AxiosError('storage refresh failed', '', config, undefined, {
          ...response,
          status: code,
        })
      }
      response.data = structuredClone(row)
    } else {
      if (hold)
        await new Promise<void>((resolve) => {
          release = resolve
        })
      if (failure) {
        const code = failure
        failure = 0
        throw new AxiosError('unsafe storage error', '', config, undefined, {
          ...response,
          status: code,
          data: { message: 'unsafe raw message' },
        })
      }
      const body = JSON.parse(String(config.data))
      if (config.url === '/admin/storage/test') response.data = structuredClone(probe)
      else if (config.url === '/admin/storage/rollback') {
        const selected = row.revisions.find((item) => item.id === body.revision_id)!
        row = { ...row, enabled: body.enabled, etag: 'revision_rollback', revision: selected }
        response.data = structuredClone(row)
      } else {
        const revision: StorageRevision = {
          id: 'str_next',
          endpoint: body.endpoint,
          region: body.region,
          bucket: body.bucket,
          prefix: body.prefix,
          credentials_configured: body.auth.action !== 'remove',
          verified_at: body.enabled ? '2026-09-29T12:00:00Z' : null,
          created_at: '2026-09-29T11:59:00Z',
        }
        row = {
          enabled: body.enabled,
          etag: 'revision_next',
          revision,
          revisions: body.enabled ? [revision, ...row.revisions] : row.revisions,
        }
        response.data = structuredClone(row)
      }
    }
    return response
  }
})

afterEach(async () => {
  release?.()
  await act(async () => root.unmount())
  host.remove()
  cache.clear()
  client.defaults.adapter = original
})

async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1))
    })
    check()
  })
}

async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <StoragePage />
      </QueryClientProvider>,
    )
  })
  await until(() => expect(cache.getQueryData(['permissions', 'usr_storage'])).toEqual(permissions))
  if (permissions.includes('storage.read'))
    await until(() => expect(cache.getQueryData(['admin', 'storage'])).toBeDefined())
}

function buttons(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
}

function button(label: string, last = false) {
  const matches = buttons(label)
  expect(matches.length, label).toBeGreaterThan(0)
  return last ? matches.at(-1)! : matches[0]
}

async function click(label: string, last = false) {
  await act(async () => button(label, last).click())
}

async function input(name: string, value: string) {
  const field = document.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  expect(field).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

async function select(name: string, value: string) {
  const field = document.querySelector<HTMLSelectElement>(`select[name="${name}"]`)!
  await act(async () => {
    field.value = value
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}

async function submit() {
  await act(async () => {
    document
      .querySelector('form[aria-label="Object storage configuration"]')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

const writes = () => requests.filter((request) => request.method !== 'get')
const payload = (index = -1) => JSON.parse(String(writes().at(index)!.data))

describe('storage settings', () => {
  it('matches the Mockup overview and keeps read permission isolated from write and test', async () => {
    permissions = ['storage.read', 'storage.test']
    row.enabled = false
    await mount()
    expect(document.querySelector('[aria-label="Storage overview"]')).toBeTruthy()
    expect(document.body.textContent).toContain('File storage')
    expect(document.body.textContent).toContain('AWS S3')
    expect(document.body.textContent).toContain('routex-files')
    await click('Configure storage')
    expect(document.querySelector('fieldset')!.disabled).toBe(true)
    expect(buttons('Save configuration')).toHaveLength(0)
    expect(button('Test storage').disabled).toBe(false)
    await click('Test storage')
    await until(() => expect(document.body.textContent).toContain('Storage verification passed'))
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/storage/test')
    expect(payload()).toEqual({ etag: 'revision_1' })

    permissions = []
    await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
    await until(() => expect(document.body.textContent).toContain('Access denied'))
    const reads = requests.filter((request) => request.url === '/admin/storage')
    expect(reads).toHaveLength(1)
  })

  it('keeps replacement credentials transient and binds saved credentials to the endpoint', async () => {
    await mount()
    await click('Configure storage')
    await input('storage_endpoint', 'https://other.example.test')
    expect(button('Save configuration').disabled).toBe(true)
    expect(button('Test storage').disabled).toBe(true)
    expect(button('Restore revision').disabled).toBe(true)
    expect(document.body.textContent).toContain('saved credentials cannot be reused')
    await select('auth_action', 'replace')
    await input('storage_access_key', 'new-access')
    await input('storage_secret_key', 'drawer-secret')
    await input('storage_endpoint', 'http://other.example.test')
    expect(button('Save configuration').disabled).toBe(false)
    await click('Cancel')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain(
      'drawer-secret',
    )

    await click('Configure storage')
    await select('auth_action', 'replace')
    expect((document.querySelector('[name="storage_access_key"]') as HTMLInputElement).value).toBe(
      '',
    )
    expect((document.querySelector('[name="storage_secret_key"]') as HTMLInputElement).value).toBe(
      '',
    )
    await input('storage_endpoint', 'http://other.example.test')
    await input('storage_access_key', 'new-access')
    await input('storage_secret_key', 'saved-secret')
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(payload()).toEqual({
      enabled: true,
      endpoint: 'http://other.example.test',
      region: 'us-east-1',
      bucket: 'routex-files',
      prefix: 'attachments/',
      auth: { action: 'replace', access_key: 'new-access', secret_key: 'saved-secret' },
      etag: 'revision_1',
    })
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-storage')
    expect(document.body.textContent).toContain('http://other.example.test')
  })

  it('preserves a draft through conflict and uncertain-result review while preventing duplicates', async () => {
    await mount()
    await click('Configure storage')
    await input('storage_prefix', 'draft/')
    failure = 409
    await submit()
    await until(() => expect(document.body.textContent).toContain('changed after this page loaded'))
    expect((document.querySelector('[name="storage_prefix"]') as HTMLInputElement).value).toBe(
      'draft/',
    )
    row = { ...row, etag: 'revision_2' }
    await click('Reload and review')
    await until(() => expect(document.body.textContent).toContain('Current revision'))

    failure = 503
    await submit()
    await until(() => expect(document.body.textContent).toContain('result is uncertain'))
    await click('Reload and review')
    await until(() => expect(button('Save configuration').disabled).toBe(false))

    hold = true
    await submit()
    await submit()
    expect(writes()).toHaveLength(3)
    expect(payload().etag).toBe('revision_2')
    release?.()
    hold = false
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  })

  it('keeps writes blocked when conflict reconciliation cannot reload current state', async () => {
    await mount()
    await click('Configure storage')
    await input('storage_prefix', 'blocked-draft/')
    failure = 409
    await submit()
    await until(() => expect(document.body.textContent).toContain('changed after this page loaded'))

    readFailure = 503
    await click('Reload and review')
    await until(() => expect(document.body.textContent).toContain('could not be reloaded'))
    expect(button('Save configuration').disabled).toBe(true)
    expect(buttons('Reload and review')).toHaveLength(1)
    expect((document.querySelector('[name="storage_prefix"]') as HTMLInputElement).value).toBe(
      'blocked-draft/',
    )
  })

  it('shows verification failure without replacing the active configuration', async () => {
    await mount()
    await click('Configure storage')
    await input('storage_prefix', 'unverified/')
    failure = 422
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('previously active configuration'),
    )
    expect(document.body.textContent).toContain('Verification did not complete successfully')
    expect(row.revision?.id).toBe('str_current')
    expect(row.etag).toBe('revision_1')
    expect((document.querySelector('[name="storage_prefix"]') as HTMLInputElement).value).toBe(
      'unverified/',
    )
  })

  it('renders measured probe stages and requires explicit rollback confirmation with exact revision', async () => {
    await mount()
    await click('Configure storage')
    await click('Test storage')
    await until(() => expect(document.body.textContent).toContain('Write probe object'))
    expect(document.body.textContent).toContain('Read probe object')
    expect(document.body.textContent).toContain('Delete probe object')
    await click('Restore revision')
    expect(writes()).toHaveLength(1)
    expect(document.body.textContent).toContain('Restore storage revision')
    await click('Restore revision', true)
    await until(() => expect(document.body.textContent).toContain('Storage revision restored'))
    expect(writes()).toHaveLength(2)
    expect(writes()[1].url).toBe('/admin/storage/rollback')
    expect(payload()).toEqual({
      revision_id: 'str_previous',
      etag: 'revision_1',
      enabled: true,
    })
    expect(document.body.textContent).toContain('https://archive.example.test')
    await select('auth_action', 'replace')
    expect((document.querySelector('[name="storage_access_key"]') as HTMLInputElement).value).toBe(
      '',
    )
    expect((document.querySelector('[name="storage_secret_key"]') as HTMLInputElement).value).toBe(
      '',
    )
  })

  it('renders an explicit empty state when there are no verified revisions', async () => {
    row = { ...row, revision: null, revisions: [], enabled: false }
    await mount()
    await click('Configure storage')
    expect(document.body.textContent).toContain('No verified revisions are available')
    expect(button('Test storage').disabled).toBe(true)
  })

  it('switches the open drawer live without clearing its draft', async () => {
    await mount()
    await click('Configure storage')
    await input('storage_prefix', 'draft-kept/')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('文件存储')
    expect(document.body.textContent).toContain('对象存储配置')
    expect((document.querySelector('[name="storage_prefix"]') as HTMLInputElement).value).toBe(
      'draft-kept/',
    )
  })

  it('keeps English and Chinese catalogs structurally aligned', () => {
    function entries(value: object, prefix = ''): string[] {
      return Object.entries(value)
        .flatMap(([key, item]) =>
          typeof item === 'object'
            ? entries(item, `${prefix}${key}.`)
            : [`${prefix}${key}:${(String(item).match(/{{.*?}}/g) ?? []).sort().join(',')}`],
        )
        .sort()
    }
    expect(entries(en)).toEqual(entries(zh))
  })
})
