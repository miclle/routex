import { StrictMode, act, useLayoutEffect, type ComponentProps } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import { sessionKey } from '@/hooks/use-auth'
import { ArchiveCleanup } from './archive-cleanup'

const queuedButtons = vi.hoisted(() => new Map<string, () => void>())
vi.mock('@/components/ui/button', () => ({
  Button: function TestButton(props: ComponentProps<'button'>) {
    useLayoutEffect(() => {
      if (typeof props.children === 'string' && props.onClick)
        queuedButtons.set(props.children, props.onClick as () => void)
    })
    return <button {...props} />
  },
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const id = 'rcv_00000000000000000000000001'
const etag = 'a'.repeat(64)
let root: Root, host: HTMLDivElement, cache: QueryClient
let authorized: boolean,
  canWrite: boolean,
  failApply: boolean,
  hold: boolean,
  release: (() => void) | undefined
let requests: InternalAxiosRequestConfig[], completed: number, locked: boolean, selected: string[]
let authority: string
let held: (() => void)[]
function render() {
  root.render(
    <StrictMode>
      <QueryClientProvider client={cache}>
        <ArchiveCleanup
          authority={authority}
          receiptAuthority={authority}
          selected={selected}
          isCurrent={() => authorized}
          canWrite={() => canWrite}
          onLock={(value) => {
            locked = value
          }}
          onComplete={() => {
            completed++
            selected = []
            render()
          }}
        />
      </QueryClientProvider>
    </StrictMode>,
  )
}
async function click(text: string) {
  const button = [...host.querySelectorAll('button')].find((value) => value.textContent === text)
  expect(button).toBeDefined()
  await act(async () => button?.click())
}
async function reason(value: string) {
  const input = host.querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
beforeEach(async () => {
  queuedButtons.clear()
  await i18n.changeLanguage('en')
  authority = 'initial'
  held = []
  authorized = true
  canWrite = true
  failApply = false
  hold = false
  release = undefined
  requests = []
  completed = 0
  locked = false
  selected = [id]
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: 'usr_operator', role: 'admin' },
    csrf_token: 'csrf',
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ etag: `"${etag}"` }),
      data: {} as unknown,
    }
    if (config.url?.endsWith('/preview'))
      response.data = {
        scope: 'archive_storage_only',
        archive_ids: JSON.parse(config.data as string).archive_ids,
        retained_count: 3,
        encrypted_bytes: 500,
        selected_count: 1,
        selected_encrypted_bytes: 100,
        version_cap: 100,
        encrypted_byte_cap: 268435456,
      }
    else {
      if (hold)
        await new Promise<void>((resolve) => {
          release = resolve
          held.push(resolve)
        })
      if (failApply)
        throw new AxiosError('private', '', config, undefined, { ...response, status: 503 })
      const input = JSON.parse(config.data as string)
      response.data = {
        scope: 'archive_storage_only',
        request_id: input.request_id,
        archive_ids: JSON.parse(config.data as string).archive_ids,
        deleted_count: 1,
        deleted_encrypted_bytes: 100,
        completed_at: '2026-10-14T09:01:00Z',
      }
    }
    return response
  }
  await act(async () => render())
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
    held.forEach((resolve) => resolve())
  })
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
})
describe('explicit administrator archive cleanup', () => {
  it('previews without deletion, confirms once and retains completed notice after selection reset', async () => {
    await click(en.archiveCleanupPreview)
    expect(requests).toHaveLength(1)
    expect(completed).toBe(0)
    await reason('Remove reviewed archive')
    await click(en.archiveCleanupConfirm)
    expect(requests).toHaveLength(2)
    expect(completed).toBe(1)
    expect(locked).toBe(false)
    expect(host.textContent).toContain(i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }))
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('retains original uncertain intent and exact review token on explicit retry', async () => {
    await click(en.archiveCleanupPreview)
    await reason('Original reason')
    failApply = true
    await click(en.archiveCleanupConfirm)
    expect(completed).toBe(0)
    expect(locked).toBe(true)
    const original = requests[1]
    expect(host.textContent).toContain(en.archiveCleanupUncertain)
    failApply = false
    await click(en.archiveCleanupRetry)
    expect(requests[2].data).toBe(original.data)
    expect(requests[2].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(completed).toBe(1)
  })
  it('withholds late completion after authority loss without claiming success', async () => {
    await click(en.archiveCleanupPreview)
    await reason('Reviewed reason')
    hold = true
    await click(en.archiveCleanupConfirm)
    expect(release).toBeDefined()
    await act(async () => {
      authorized = false
      render()
      release?.()
    })
    expect(completed).toBe(0)
    expect(host.textContent).not.toContain(en.archiveCleanupTitle)
    expect(locked).toBe(true)
  })
  it('releases an obsolete held write for exact fresh-CSRF retry and ignores its late finally', async () => {
    await click(en.archiveCleanupPreview)
    await reason('Original retained reason')
    hold = true
    await click(en.archiveCleanupConfirm)
    expect(held).toHaveLength(1)
    const original = requests[1]
    await act(async () => {
      authorized = false
      authority = 'renewing'
      render()
    })
    expect(host.textContent).not.toContain(en.archiveCleanupTitle)
    expect(locked).toBe(true)
    await act(async () => {
      cache.setQueryData(sessionKey, {
        user: { id: 'usr_operator', role: 'admin' },
        csrf_token: 'fresh-csrf',
      })
      authorized = true
      authority = 'renewed'
      render()
    })
    expect(host.querySelector('input')?.value).toBe('Original retained reason')
    await click(en.archiveCleanupRetry)
    expect(held).toHaveLength(2)
    expect(requests[2].data).toBe(original.data)
    expect(requests[2].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(requests[2].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    await act(async () => held[0]())
    expect(completed).toBe(0)
    expect(locked).toBe(true)
    expect(host.textContent).toContain(en.archiveCleanupPending)
    expect(host.querySelector('input')?.value).toBe('Original retained reason')
    await click(en.archiveCleanupRetry)
    expect(requests).toHaveLength(3)
    await act(async () => held[1]())
    expect(completed).toBe(1)
    expect(locked).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('validates a pasted control character before capturing intent and localizes the accessible error', async () => {
    await click(en.archiveCleanupPreview)
    await reason('Unsafe\u0001reason')
    await click(en.archiveCleanupConfirm)
    expect(requests).toHaveLength(1)
    expect(locked).toBe(false)
    const input = host.querySelector('input')!
    expect(input.getAttribute('aria-invalid')).toBe('true')
    const errorID = input.getAttribute('aria-describedby')!
    expect(document.getElementById(errorID)?.textContent).toBe(en.archiveCleanupInvalidReason)
    expect(document.getElementById(errorID)?.getAttribute('role')).toBe('alert')
    expect(input.disabled).toBe(false)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.getElementById(errorID)?.textContent).toBe(zh.archiveCleanupInvalidReason)
    expect(input.value).toBe('Unsafe\u0001reason')
    await reason('Corrected reviewed reason')
    expect(input.getAttribute('aria-invalid')).toBe('false')
    await click(zh.archiveCleanupConfirm)
    expect(requests).toHaveLength(2)
    expect(JSON.parse(requests[1].data).reason).toBe('Corrected reviewed reason')
    expect(completed).toBe(1)
  })
  it('does not dispatch without independent write authority', async () => {
    await act(async () => {
      canWrite = false
      render()
    })
    expect(host.querySelector('button')).toBeNull()
    expect(requests).toHaveLength(0)
  })
  it('switches pending review language without repeating a read or clearing reason', async () => {
    await click(en.archiveCleanupPreview)
    await reason('Keep this draft')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain(zh.archiveCleanupTitle)
    expect(host.querySelector('input')?.value).toBe('Keep this draft')
    expect(requests).toHaveLength(1)
  })
})

it('revokes the initial queued cleanup callback after an uncertain response', async () => {
  await click(en.archiveCleanupPreview)
  await reason('Original reviewed cleanup')
  const queued = queuedButtons.get(en.archiveCleanupConfirm)!
  failApply = true
  await act(async () => queued())
  expect(requests).toHaveLength(2)
  expect(host.textContent).toContain(en.archiveCleanupUncertain)
  await act(async () => queued())
  expect(requests).toHaveLength(2)
  failApply = false
  await click(en.archiveCleanupRetry)
  expect(requests).toHaveLength(3)
  expect(requests[2].data).toBe(requests[1].data)
  expect(completed).toBe(1)
})
it('requires a current explicit retry after each repeated uncertain response', async () => {
  await click(en.archiveCleanupPreview)
  await reason('Repeated uncertain cleanup')
  failApply = true
  await click(en.archiveCleanupConfirm)
  const oldRetry = queuedButtons.get(en.archiveCleanupRetry)!
  await act(async () => oldRetry())
  expect(requests).toHaveLength(3)
  await act(async () => oldRetry())
  expect(requests).toHaveLength(3)
  failApply = false
  await click(en.archiveCleanupRetry)
  expect(requests).toHaveLength(4)
  expect(requests[3].data).toBe(requests[1].data)
  expect(completed).toBe(1)
})
it('revokes the old preview callback when another archive selection is reviewed', async () => {
  await click(en.archiveCleanupPreview)
  await reason('First archive review')
  const oldConfirm = queuedButtons.get(en.archiveCleanupConfirm)!
  const replacement = 'rcv_00000000000000000000000002'
  await act(async () => {
    selected = [replacement]
    render()
  })
  await click(en.archiveCleanupPreview)
  await reason('Second archive review')
  await act(async () => oldConfirm())
  expect(requests).toHaveLength(2)
  expect(completed).toBe(0)
  await click(en.archiveCleanupConfirm)
  expect(requests).toHaveLength(3)
  expect(JSON.parse(requests[2].data).archive_ids).toEqual([replacement])
  expect(JSON.parse(requests[2].data).reason).toBe('Second archive review')
  expect(completed).toBe(1)
})
