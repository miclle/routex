import { act, StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyRuntimeArchiveRollback,
  getRuntimeArchiveRollbackReceipt,
  previewRuntimeArchiveRollback,
} from '@/api/runtime-archive-rollback'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  RuntimeArchiveRollbackInput,
  RuntimeArchiveRollbackPreview,
  RuntimeArchiveRollbackReceipt,
} from '@/types/runtime-archive-rollback'
import { ArchiveRollback } from './archive-rollback'
const queuedButtons = vi.hoisted(() => new Map<string, () => void>())
vi.mock('@/components/ui/button', async (original) => {
  const { Button } = await original<typeof import('@/components/ui/button')>()
  return {
    Button: (props: React.ComponentProps<typeof Button>) => {
      if (typeof props.children === 'string' && props.onClick)
        queuedButtons.set(props.children, props.onClick as () => void)
      return <Button {...props} />
    },
  }
})
vi.mock('@/api/runtime-archive-rollback', async (original) => ({
  ...(await original<object>()),
  applyRuntimeArchiveRollback: vi.fn(),
  getRuntimeArchiveRollbackReceipt: vi.fn(),
  previewRuntimeArchiveRollback: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const archiveID = 'rcv_00000000000000000000000002',
  birth = '2026-10-14T09:00:00Z',
  etag = 'a'.repeat(64)
const session: Session = {
  user: { id: 'usr_operator', name: 'Operator', email: 'operator@example.test', role: 'admin' },
  csrf_token: 'first-csrf',
}
const transport: RuntimeArchiveRollbackPreview['connections'][number]['before'] = {
  base_url: 'https://before.example.test/v1',
  protocol: 'openai_chat',
  adapter: 'native',
  api_version: null,
  egress_mode: 'direct',
  egress_id: null,
}
const seed: RuntimeArchiveRollbackPreview = {
  scope: 'gateway_routing_configuration',
  archive_id: archiveID,
  archive_created_at: birth,
  observed_at: birth,
  can_apply: true,
  blocker_codes: [],
  etag,
  connections: [
    {
      connection_id: 'con_exact',
      name: 'Saved transport',
      enabled: false,
      before: transport,
      after: { ...transport, base_url: 'https://after.example.test/v1' },
      requires_verification: true,
    },
  ],
  declarations: [
    {
      provider_model_id: 'pmd_exact',
      before: { image_input: true, pdf_input: false },
      after: { image_input: false, pdf_input: true },
    },
  ],
  weights: [
    {
      binding_id: 'bnd_exact',
      model_id: 'mdl_exact',
      provider_model_id: 'pmd_exact',
      protocol: 'openai_chat',
      before: 20,
      after: 80,
    },
  ],
}
function receipt(input: RuntimeArchiveRollbackInput): RuntimeArchiveRollbackReceipt {
  return {
    scope: 'gateway_routing_configuration',
    request_id: input.request_id,
    archive_id: archiveID,
    archive_created_at: birth,
    applied_at: birth,
    changed: true,
    reason: input.reason,
    publication: {
      request_id: input.request_id,
      actor_id: session.user.id,
      reason: input.reason,
      claim_instance_id: 'ins_exact',
      claim_instance_started_at: birth,
      claimed_at: birth,
      finalized_at: null,
      status: 'unknown',
      failure_code: null,
    },
    runtime_applied: true,
    superseded: false,
  }
}
let root: Root, host: HTMLDivElement, cache: QueryClient
let authority: string,
  actorID: string,
  archiveBirth: string,
  readable: boolean,
  writable: boolean,
  closed: boolean,
  unmounted: boolean
let releases: ((value: RuntimeArchiveRollbackReceipt) => void)[]
function render() {
  const captured = authority
  root.render(
    <StrictMode>
      <QueryClientProvider client={cache}>
        {!closed && (
          <ArchiveRollback
            actorID={actorID}
            authority={authority}
            archiveID={archiveID}
            archiveBirth={archiveBirth}
            canRead={() => readable && captured === authority}
            canWrite={() => writable && captured === authority}
            onClose={() => {
              closed = true
              render()
            }}
          />
        )}
      </QueryClientProvider>
    </StrictMode>,
  )
}
function button(text: string) {
  const value = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent === text,
  )
  if (!value) throw new Error(`Missing button ${text}`)
  return value
}
async function click(text: string) {
  await act(async () => button(text).click())
}
async function reason(value: string) {
  await act(async () => {
    const input = document.querySelector('input')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function reviewed() {
  await click(en.archiveRollbackPreview)
  await reason('  Original reviewed reason  ')
}
async function uncertain() {
  await reviewed()
  vi.mocked(applyRuntimeArchiveRollback).mockRejectedValueOnce(new Error('Unsafe private error'))
  await click(en.archiveRollbackConfirm)
  expect(document.body.textContent).toContain(en.archiveRollbackIssue_uncertain)
  return vi.mocked(applyRuntimeArchiveRollback).mock.calls[0]
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  queuedButtons.clear()
  authority = 'initial'
  actorID = session.user.id
  archiveBirth = birth
  readable = true
  writable = true
  closed = false
  unmounted = false
  releases = []
  vi.mocked(previewRuntimeArchiveRollback).mockResolvedValue(structuredClone(seed))
  vi.mocked(applyRuntimeArchiveRollback).mockImplementation(async (_id, input) => receipt(input))
  vi.mocked(getRuntimeArchiveRollbackReceipt).mockRejectedValue(new Error('Missing receipt'))
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, structuredClone(session))
  await act(async () => render())
})
afterEach(async () => {
  await act(async () => {
    if (!unmounted) root.unmount()
    releases.forEach((resolve) =>
      resolve(receipt({ request_id: crypto.randomUUID(), reason: 'stale' })),
    )
  })
  cache.clear()
  host.remove()
})
describe('explicit reviewed Gateway routing rollback', () => {
  it('revokes the initial confirmation after an uncertain rollback', async () => {
    await reviewed()
    const oldConfirm = queuedButtons.get(en.archiveRollbackConfirm)!
    vi.mocked(applyRuntimeArchiveRollback).mockRejectedValueOnce(new Error('Response lost'))
    await act(async () => oldConfirm())
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await act(async () => oldConfirm())
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await click(en.archiveRollbackRetry)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(2)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[1][1]).toBe(
      vi.mocked(applyRuntimeArchiveRollback).mock.calls[0][1],
    )
  })
  it('requires a fresh retry callback after each uncertain response', async () => {
    const original = await uncertain()
    const oldRetry = queuedButtons.get(en.archiveRollbackRetry)!
    vi.mocked(applyRuntimeArchiveRollback).mockRejectedValueOnce(new Error('Response lost again'))
    await act(async () => oldRetry())
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(2)
    await act(async () => oldRetry())
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(2)
    await click(en.archiveRollbackRetry)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(3)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[2][1]).toBe(original[1])
  })
  it('rejects a queued confirmation after the reviewed reason changes', async () => {
    await reviewed()
    const oldConfirm = queuedButtons.get(en.archiveRollbackConfirm)!
    await reason('Replacement reviewed reason')
    await act(async () => oldConfirm())
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    await click(en.archiveRollbackConfirm)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[0][1].reason).toBe(
      'Replacement reviewed reason',
    )
  })
  it('rejects a queued confirmation after a new preview replaces its ETag', async () => {
    await reviewed()
    const oldConfirm = queuedButtons.get(en.archiveRollbackConfirm)!
    vi.mocked(previewRuntimeArchiveRollback).mockResolvedValueOnce({
      ...seed,
      etag: 'b'.repeat(64),
    })
    await click(en.archiveRollbackPreview)
    await act(async () => oldConfirm())
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    await click(en.archiveRollbackConfirm)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[0][2]).toBe('b'.repeat(64))
  })
  it('revokes queued writes immediately when the rollback dialog closes', async () => {
    await reviewed()
    const oldConfirm = queuedButtons.get(en.archiveRollbackConfirm)!
    await click(en.routingClose)
    await act(async () => oldConfirm())
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
  })
  it('labels a stored legacy empty default mode in both languages without rewriting the preview', async () => {
    const legacy = structuredClone(seed)
    legacy.connections[0].before.egress_mode = ''
    legacy.connections[0].after.egress_mode = ''
    vi.mocked(previewRuntimeArchiveRollback).mockResolvedValue(legacy)
    await click(en.archiveRollbackPreview)
    expect(document.body.textContent).toContain(en.archiveRollbackLegacyDefault)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain(zh.archiveRollbackLegacyDefault)
    expect(previewRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    expect(legacy.connections[0].before.egress_mode).toBe('')
  })
  it('renders server-derived differences before confirmation and separates historical publication from current proof', async () => {
    expect(previewRuntimeArchiveRollback).not.toHaveBeenCalled()
    await reviewed()
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    expect(document.querySelectorAll('table')).toHaveLength(3)
    expect(document.body.textContent).toContain(transport.base_url)
    expect(document.body.textContent).toContain('https://after.example.test/v1')
    expect(document.body.textContent).toContain('20')
    expect(document.body.textContent).toContain('80')
    expect(document.body.textContent).toContain(en.archiveRollbackTransportBoundary)
    await click(en.archiveRollbackConfirm)
    const args = vi.mocked(applyRuntimeArchiveRollback).mock.calls[0]
    expect(args[0]).toBe(archiveID)
    expect(args[1]).toMatchObject({ reason: 'Original reviewed reason' })
    expect(args[1].request_id).toMatch(/^[a-f0-9-]{36}$/)
    expect(args[2]).toBe(etag)
    expect(args[3]).toBe(session.csrf_token)
    expect(document.body.textContent).toContain(en.archiveRollbackRecorded)
    expect(document.body.textContent).toContain(en.archiveRollbackPublication_unknown)
    expect(document.body.textContent).toContain(en.archiveRollbackCurrentApplied)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      cache
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey),
    ).toEqual([sessionKey])
  })
  it('allows receipt refresh with read-only authority without redispatch', async () => {
    const original = await uncertain()
    await act(async () => {
      writable = false
      authority = 'readonly'
      render()
    })
    expect(document.body.textContent).not.toContain(en.archiveRollbackRetry)
    vi.mocked(getRuntimeArchiveRollbackReceipt).mockResolvedValueOnce(receipt(original[1]))
    await click(en.archiveRollbackRefreshReceipt)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain(en.archiveRollbackRecorded)
    expect(getRuntimeArchiveRollbackReceipt).toHaveBeenCalledWith(
      archiveID,
      original[1].request_id,
      expect.any(AbortSignal),
    )
  })
  it('provides read-only preview without a confirmation action', async () => {
    await act(async () => {
      writable = false
      render()
    })
    await click(en.archiveRollbackPreview)
    expect(document.querySelectorAll('table')).toHaveLength(3)
    expect(document.body.textContent).not.toContain(en.archiveRollbackConfirm)
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
  })
  it.each(['new_future_blocker', 'connection_enabled', 'unknown_capability'])(
    'fails closed for blocker %s',
    async (code) => {
      vi.mocked(previewRuntimeArchiveRollback).mockResolvedValueOnce({
        ...seed,
        blocker_codes: [code],
        can_apply: false,
      })
      await reviewed()
      expect(document.body.textContent).toContain(en.archiveRollbackBlocked)
      expect(document.body.textContent).not.toContain(code)
      expect(button(en.archiveRollbackConfirm).disabled).toBe(true)
      await click(en.archiveRollbackConfirm)
      expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    },
  )
  it('requires explicit fresh preview after a review generation changes', async () => {
    await reviewed()
    await act(async () => {
      authority = 'renewed'
      render()
    })
    expect(document.querySelectorAll('table')).toHaveLength(0)
    expect(document.body.textContent).not.toContain(en.archiveRollbackConfirm)
    expect(previewRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await click(en.archiveRollbackPreview)
    expect(document.querySelector('input')?.value).toBe('  Original reviewed reason  ')
    await click(en.archiveRollbackConfirm)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
  })
  it('retains exact original UUID, reason and ETag through renewal and retries only with fresh CSRF', async () => {
    const original = await uncertain()
    await act(async () => {
      readable = false
      authority = 'renewing'
      render()
    })
    expect(document.body.textContent).not.toContain(archiveID)
    expect(document.body.textContent).not.toContain(en.archiveRollbackRetry)
    await act(async () => {
      cache.setQueryData(sessionKey, { ...session, csrf_token: 'fresh-csrf' })
      readable = true
      authority = 'renewed'
      render()
    })
    expect(document.querySelector('input')?.value).toBe('Original reviewed reason')
    expect(document.querySelector('input')?.disabled).toBe(true)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await click(en.archiveRollbackRetry)
    const retried = vi.mocked(applyRuntimeArchiveRollback).mock.calls[1]
    expect(retried[1]).toBe(original[1])
    expect(retried[2]).toBe(original[2])
    expect(retried[3]).toBe('fresh-csrf')
    expect(previewRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
  })
  it('retains original intent after a missing receipt without automatically mutating', async () => {
    const original = await uncertain()
    await click(en.archiveRollbackRefreshReceipt)
    expect(document.body.textContent).toContain(en.archiveRollbackIssue_receipt)
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await click(en.archiveRollbackRetry)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[1][1]).toBe(original[1])
  })
  it.each(['archive_birth', 'actor', 'reason', 'request'] as const)(
    'rejects receipt changed %s and retains uncertain intent',
    async (change) => {
      await reviewed()
      vi.mocked(applyRuntimeArchiveRollback).mockImplementationOnce(async (_id, input) => {
        const value = receipt(input)
        if (change === 'archive_birth') value.archive_created_at = '2026-10-14T09:01:00Z'
        if (change === 'actor') value.publication.actor_id = 'usr_other'
        if (change === 'reason') value.reason = 'different reason'
        if (change === 'request') value.request_id = crypto.randomUUID()
        return value
      })
      await click(en.archiveRollbackConfirm)
      expect(document.body.textContent).toContain(en.archiveRollbackIssue_uncertain)
      expect(document.body.textContent).not.toContain(en.archiveRollbackRecorded)
      expect(button(en.archiveRollbackRetry)).toBeDefined()
    },
  )
  it('rejects preview with a changed archive birth', async () => {
    vi.mocked(previewRuntimeArchiveRollback).mockResolvedValueOnce({
      ...seed,
      archive_created_at: '2026-10-14T09:01:00Z',
    })
    await click(en.archiveRollbackPreview)
    expect(document.body.textContent).toContain(en.archiveRollbackIssue_preview)
    expect(document.querySelectorAll('table')).toHaveLength(0)
  })
  it('releases obsolete held write ownership and ignores its late completion and finally', async () => {
    await reviewed()
    vi.mocked(applyRuntimeArchiveRollback).mockImplementation(
      () => new Promise((resolve) => releases.push(resolve)),
    )
    await click(en.archiveRollbackConfirm)
    const original = vi.mocked(applyRuntimeArchiveRollback).mock.calls[0]
    await act(async () => {
      authority = 'renewed'
      render()
    })
    expect(original[4]?.aborted).toBe(true)
    await click(en.archiveRollbackRetry)
    expect(releases).toHaveLength(2)
    expect(vi.mocked(applyRuntimeArchiveRollback).mock.calls[1][1]).toBe(original[1])
    await act(async () => releases[0](receipt(original[1])))
    expect(document.body.textContent).not.toContain(en.archiveRollbackRecorded)
    expect(document.body.textContent).toContain(en.archiveRollbackPending)
    expect(button(en.archiveRollbackRetry).disabled).toBe(true)
    await act(async () => releases[1](receipt(original[1])))
    expect(document.body.textContent).toContain(en.archiveRollbackRecorded)
  })
  it('blocks duplicate synchronous dispatch', async () => {
    await reviewed()
    vi.mocked(applyRuntimeArchiveRollback).mockImplementation(
      () => new Promise((resolve) => releases.push(resolve)),
    )
    const confirm = button(en.archiveRollbackConfirm)
    await act(async () => {
      confirm.click()
      confirm.click()
    })
    expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
  })
  it.each(['permission', 'generation', 'dismissal', 'unmount'] as const)(
    'hides old preview and ignores held read on %s',
    async (change) => {
      let resolve!: (value: RuntimeArchiveRollbackPreview) => void
      vi.mocked(previewRuntimeArchiveRollback).mockImplementationOnce(
        () =>
          new Promise((done) => {
            resolve = done
          }),
      )
      await click(en.archiveRollbackPreview)
      await act(async () => {
        if (change === 'permission') {
          readable = false
          authority = 'denied'
          render()
        }
        if (change === 'generation') {
          authority = 'renewed'
          render()
        }
        if (change === 'dismissal') button(en.routingClose).click()
        if (change === 'unmount') {
          root.unmount()
          unmounted = true
        }
        resolve(seed)
      })
      expect(document.querySelectorAll('table')).toHaveLength(0)
      expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    },
  )
  it.each(['actor', 'birth', 'dismissal'] as const)(
    'clears original local intent on %s change',
    async (change) => {
      await uncertain()
      await act(async () => {
        if (change === 'actor') {
          actorID = 'usr_other'
          cache.setQueryData(sessionKey, { ...session, user: { ...session.user, id: actorID } })
          render()
        }
        if (change === 'birth') {
          archiveBirth = '2026-10-14T09:01:00Z'
          render()
        }
        if (change === 'dismissal') button(en.routingClose).click()
      })
      expect(document.body.textContent).not.toContain(en.archiveRollbackRetry)
      expect(document.querySelector('input')).toBeNull()
      expect(applyRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    },
  )
  it.each(['unknown', 'pending', 'refresh_returned_nil', 'refresh_returned_error'] as const)(
    'renders original %s separately from fresh current proof and supersession',
    async (status) => {
      await reviewed()
      vi.mocked(applyRuntimeArchiveRollback).mockImplementationOnce(async (_id, input) => ({
        ...receipt(input),
        publication: { ...receipt(input).publication, status },
        runtime_applied: false,
        superseded: true,
      }))
      await click(en.archiveRollbackConfirm)
      expect(document.body.textContent).toContain(en[`archiveRollbackPublication_${status}`])
      expect(document.body.textContent).toContain(en.archiveRollbackSuperseded)
      expect(document.body.textContent).not.toContain(en.archiveRollbackCurrentApplied)
      expect(document.body.textContent).toContain(en.archiveRollbackReceiptBoundary)
    },
  )
  it('validates reason locally and switches language without clearing draft or repeating reads', async () => {
    await click(en.archiveRollbackPreview)
    await reason('Unsafe\u0001reason')
    await click(en.archiveRollbackConfirm)
    expect(applyRuntimeArchiveRollback).not.toHaveBeenCalled()
    const input = document.querySelector('input')!
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(document.getElementById(input.getAttribute('aria-describedby')!)?.textContent).toBe(
      en.archiveRollbackInvalidReason,
    )
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain(zh.archiveRollbackInvalidReason)
    expect(input.value).toBe('Unsafe\u0001reason')
    expect(previewRuntimeArchiveRollback).toHaveBeenCalledTimes(1)
    await reason('Corrected reason')
    await click(zh.archiveRollbackConfirm)
    expect(document.body.textContent).toContain(zh.archiveRollbackRecorded)
  })
})
