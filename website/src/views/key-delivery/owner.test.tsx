import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/keyDeliveryOwner'
import zh from '@/i18n/locales/zh/keyDeliveryOwner'
import type { Session } from '@/types/auth'
import type { PersonalKey } from '@/types/catalog'
import type { KeyDeliveryOwner, KeyDeliveryReceipt } from '@/types/key-delivery'
import KeyIssueDialog from './owner'
import DeliveryReceiptDialog from './receipt'
import { useOwnerAuthority } from './lifetime'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const suffix = '01m36yee4gkbns18pfcqqc75a3'
const id = (prefix: string) => prefix + '_' + suffix
const review = 'a'.repeat(64)
const csrf = 'b'.repeat(64)
const requestId = '10000000-0000-4000-8000-000000000001'
const originalAdapter = client.defaults.adapter
const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const writeText = vi.fn<(value: string) => Promise<void>>()
let root: Root, container: HTMLDivElement, cache: QueryClient
let scope: KeyDeliveryOwner
let requests: InternalAxiosRequestConfig[]
let targetReady: boolean, issueFails: boolean, reconcileFails: boolean, receiptMissing: boolean
let held: { url: string; promise: Promise<void>; release: () => void } | null
let issuedReceipt: KeyDeliveryReceipt | null
let policyReview: string
let contextModes: ('manual' | 'managed_vault')[]
const manual = vi.fn<() => Promise<void>>()
const received = vi.fn<(value: KeyDeliveryReceipt) => void>()
const changed = vi.fn<() => void>()
const closed = vi.fn<() => void>()
const session = (): Session => ({
  user: { id: id('usr'), role: 'member', name: 'Member', email: 'member@example.test' },
  csrf_token: csrf,
})
function key(predecessor: string | null = null): PersonalKey {
  return {
    id: id(scope.kind === 'project' ? 'pky' : 'key'),
    name: 'Production',
    prefix: 'rx_masked',
    status: 'pending',
    model_ids: [id('mdl')],
    expires_at: null,
    created_at: '2026-10-11T00:00:00Z',
    replaces_key_id: predecessor,
    delivery_expires_at: null,
    delivery_mode: 'managed_vault',
  }
}
function target() {
  return {
    id: id('vap'),
    consumer_id: id('kdc'),
    application_name: 'Application',
    environment: 'production',
    status: targetReady ? 'ready' : 'pending',
    control_verified: targetReady,
    isolation: targetReady
      ? { status: 'operator_attested', revision: review, recorded_at: '2026-10-11T00:00:00Z' }
      : { status: 'missing', revision: null, recorded_at: null },
    failure_code: targetReady ? null : 'principal_control_unverified',
    review_etag: review,
  }
}
function result(config: InternalAxiosRequestConfig, runtime: 'unknown' | 'confirmed' = 'unknown') {
  const input = JSON.parse(config.data)
  const predecessor = config.url!.endsWith('/managed-rotation')
    ? config.url!.split('/').at(-2)!
    : null
  const item = {
    ...key(predecessor),
    status: runtime === 'confirmed' ? ('active' as const) : ('pending' as const),
    ...(scope.kind === 'project' ? { project_id: scope.projectId, creator_id: id('usr') } : {}),
  }
  const descriptor =
    runtime === 'confirmed'
      ? {
          schema_version: 1 as const,
          key_id: item.id,
          profile_id: id('vap'),
          principal_id: id('vpb'),
          slot_id: id('kds'),
          descriptor_revision: '9007199254740993',
          endpoint: 'https://vault.example.test',
          namespace: '',
          mount: 'kv',
          path: 'api-keys/' + id('vap') + '/routex-api-key-' + item.id,
          data_field: 'value',
          version: 1 as const,
          method: 'jwt' as const,
          auth_mount: 'jwt',
          role: 'routex-api-key-' + id('vpb'),
          coordinator: 'not_configured' as const,
          application_status: 'unknown' as const,
        }
      : null
  const delivery: KeyDeliveryReceipt = {
    request_id: input.request_id,
    key_id: item.id,
    mode: 'managed_vault',
    delivery_status: 'acknowledged',
    key_status: item.status,
    runtime_status: runtime,
    replacement_call_status: predecessor ? 'not_recorded' : 'not_applicable',
    failure_code: runtime === 'unknown' ? 'runtime_publication_unknown' : null,
    descriptor,
    review_etag: review,
  }
  issuedReceipt = delivery
  return { key: item, delivery }
}
function base() {
  return scope.kind === 'personal' ? '/keys' : '/projects/' + scope.projectId + '/keys'
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  i18n.addResourceBundle('en', 'keyDeliveryOwner', en, true, true)
  i18n.addResourceBundle('zh', 'keyDeliveryOwner', zh, true, true)
  root = createRoot((container = document.createElement('div')))
  document.body.append(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  scope = { kind: 'personal' }
  requests = []
  targetReady = false
  issueFails = false
  reconcileFails = false
  receiptMissing = false
  held = null
  issuedReceipt = null
  policyReview = review
  contextModes = ['manual', 'managed_vault']
  manual.mockReset().mockResolvedValue(undefined)
  received.mockReset()
  changed.mockReset()
  closed.mockReset()
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (held && config.url === held.url) await held.promise
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ etag: '"' + review + '"' }),
      data: {} as unknown,
    }
    if (config.url === base() + '/delivery-context') {
      response.data = {
        allowed_modes: contextModes,
        default_mode: contextModes.includes('managed_vault') ? 'managed_vault' : 'manual',
        managed_available: true,
        failure_code: null,
        review_etag: policyReview,
      }
      response.headers.set('etag', '"' + policyReview + '"')
    } else if (config.url === base() + '/delivery-targets/resolve') {
      response.status = 201
      response.data = target()
    } else if (config.url === base() + '/delivery-targets/' + id('vap')) response.data = target()
    else if (config.url?.endsWith('/managed') || config.url?.endsWith('/managed-rotation')) {
      if (issueFails) {
        response.status = 503
        throw new AxiosError(
          'private upstream secret must not render',
          '',
          config,
          undefined,
          response,
        )
      }
      response.status = 202
      response.data = result(config)
    } else if (config.url?.includes('/delivery-receipts/')) {
      if (receiptMissing) {
        response.status = 404
        throw new AxiosError('missing', '', config, undefined, response)
      }
      response.data = issuedReceipt
    } else if (config.url?.endsWith('/delivery-descriptor')) {
      if (receiptMissing) {
        response.status = 404
        throw new AxiosError('missing', '', config, undefined, response)
      }
      response.data = issuedReceipt?.descriptor
    } else if (config.url?.endsWith('/delivery-reconcile')) {
      if (reconcileFails) {
        response.status = 503
        throw new AxiosError('private-reconcile-detail', '', config, undefined, response)
      }
      const replacementCallStatus = issuedReceipt?.replacement_call_status
      response.data = result(config, 'confirmed')
      if (replacementCallStatus && issuedReceipt)
        issuedReceipt.replacement_call_status = replacementCallStatus
    } else throw new Error('Unexpected request')
    return response
  }
})
afterEach(async () => {
  held?.release()
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
  if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
  await i18n.changeLanguage('en')
})
function Harness({
  predecessor,
  receipt = false,
  allowed = true,
  receiptKeyId,
  receiptRequestId,
  descriptorOnly = false,
}: {
  predecessor?: PersonalKey
  receipt?: boolean
  allowed?: boolean
  receiptKeyId?: string
  receiptRequestId?: string
  descriptorOnly?: boolean
}) {
  const auth = useQuery({
    queryKey: ['auth', 'session'],
    queryFn: async () => session(),
    initialData: session,
  })
  useQuery({
    queryKey:
      scope.kind === 'personal'
        ? ['keys', auth.data?.user.id ?? '']
        : ['project-keys', scope.projectId, auth.data?.user.id ?? ''],
    queryFn: async () => [],
    initialData: [],
  })
  useQuery({
    queryKey:
      scope.kind === 'personal'
        ? ['models', auth.data?.user.id ?? '']
        : ['permissions', auth.data?.user.id ?? ''],
    queryFn: async () => [],
    initialData: [],
  })
  const authority = useOwnerAuthority(scope, allowed)
  return receipt ? (
    <DeliveryReceiptDialog
      owner={scope}
      authority={authority}
      requestId={
        descriptorOnly ? undefined : (receiptRequestId ?? issuedReceipt?.request_id ?? requestId)
      }
      keyId={receiptKeyId ?? key().id}
      onClose={closed}
      onChanged={changed}
    />
  ) : (
    <KeyIssueDialog
      owner={scope}
      authority={authority}
      predecessor={predecessor}
      models={[{ id: id('mdl'), name: 'Model' }]}
      title="Create Key"
      description="Reviewed owner scope"
      manualLabel="Create and reveal key"
      onManual={manual}
      onReceipt={received}
      onClose={closed}
    />
  )
}
async function render(props: Parameters<typeof Harness>[0] = {}) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Harness {...props} />
      </QueryClientProvider>,
    ),
  )
}
async function until(check: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 59) throw error
    }
  }
}
function button(text: string) {
  const item = [...document.querySelectorAll('button')].find((item) => item.textContent === text)
  expect(item, text).toBeDefined()
  return item!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
function capturedClick(node: HTMLButtonElement) {
  const property = Object.keys(node).find((key) => key.startsWith('__reactProps$'))!
  const props = (node as unknown as Record<string, { onClick: () => void }>)[property]
  expect(props.onClick).toBeTypeOf('function')
  return props.onClick
}
async function fill(name: string, value: string) {
  await act(async () => {
    const item = document.querySelector<HTMLInputElement>('input[name="' + name + '"]')!
    expect(item).not.toBeNull()
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(item, value)
    item.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
async function setup(predecessor?: PersonalKey) {
  await render({ predecessor })
  await until(() =>
    expect(document.querySelector('select[aria-label="Delivery method"]')).not.toBeNull(),
  )
  if (!predecessor) {
    await fill('name', 'Production')
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[name="model_ids"]')!.click(),
    )
  }
  await fill('application_name', 'Application')
  await fill('environment', 'production')
  await fill('subject', 'exact-workload-subject')
  await fill('reason', 'Reviewed deployment')
  await click('Review application target')
  await click('Confirm')
  await until(() => expect(document.body.textContent).toContain('Application ID'))
}
async function ready(predecessor?: PersonalKey) {
  await setup(predecessor)
  targetReady = true
  await click('Read current facts')
  await until(() =>
    expect(document.body.textContent).toContain('Ready according to current server checks'),
  )
}
function post(suffix: string) {
  return requests.filter((item) => item.method === 'post' && item.url?.endsWith(suffix))
}
function caches() {
  return JSON.stringify({
    queries: cache
      .getQueryCache()
      .getAll()
      .map((q) => q.state),
    mutations: cache
      .getMutationCache()
      .getAll()
      .map((m) => m.state),
  })
}
function hold(url: string) {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  held = { url, promise, release }
  return held
}

describe('owner managed Key delivery', () => {
  it('keeps the owner delivery catalogs paired with exact interpolation names', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort())
    for (const name of Object.keys(en) as (keyof typeof en)[]) {
      expect(en[name].match(/\{\{[^}]+\}\}/g) ?? []).toEqual(zh[name].match(/\{\{[^}]+\}\}/g) ?? [])
    }
  })
  it('resolves an explicit JWT target without fabricating approval, then issues only after current server readiness and confirmation', async () => {
    await setup()
    expect(post('/managed')).toHaveLength(0)
    expect(document.body.textContent).toContain('Pending verification and approvals')
    expect(button('Review managed delivery').disabled).toBe(true)
    const resolve = JSON.parse(post('/delivery-targets/resolve')[0].data)
    expect(resolve.consumer_id).toBe('')
    expect(resolve.principal).toEqual({ method: 'jwt', subject: 'exact-workload-subject' })
    targetReady = true
    await click('Read current facts')
    await until(() => expect(button('Review managed delivery').disabled).toBe(false))
    await submit()
    expect(post('/managed')).toHaveLength(0)
    await click('Confirm')
    await until(() => expect(received).toHaveBeenCalledOnce())
    const sent = JSON.parse(post('/managed')[0].data)
    expect(sent.delivery.consumer_id).toBe(id('kdc'))
    expect(sent.delivery.target_review).toBe(review)
    expect(sent.model_ids).toEqual([id('mdl')])
    expect(sent.request_id).toMatch(/^[0-9a-f-]{36}$/)
    expect(received.mock.calls[0][0].delivery_status).toBe('acknowledged')
    expect(received.mock.calls[0][0].runtime_status).toBe('unknown')
    expect(caches()).not.toContain('exact-workload-subject')
    expect(caches()).not.toContain(sent.request_id)
    expect(manual).not.toHaveBeenCalled()
    expect(localStorage.getItem('routex.key')).toBeNull()
    expect(sessionStorage.length).toBe(0)
  })
  it('retains the exact uncertain resolve UUID and body after renewed facts and Session', async () => {
    const h = hold(base() + '/delivery-targets/resolve')
    await render()
    await until(() => expect(document.querySelector('input[name="name"]')).not.toBeNull())
    await fill('application_name', 'Application')
    await fill('environment', 'production')
    await fill('subject', 'exact-workload-subject')
    await fill('reason', 'Original reason')
    await click('Review application target')
    await click('Confirm')
    await until(() => expect(post('/delivery-targets/resolve')).toHaveLength(1))
    const first = post('/delivery-targets/resolve')[0]
    const original = first.data
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'c'.repeat(64) }),
    )
    expect(first.signal?.aborted).toBe(true)
    h.release()
    await until(() =>
      expect(
        [...document.querySelectorAll('button')].some(
          (x) => x.textContent === 'Retry original request' && !x.disabled,
        ),
      ).toBe(true),
    )
    await click('Read current facts')
    await click('Retry original request')
    await click('Confirm')
    await until(() => expect(post('/delivery-targets/resolve')).toHaveLength(2))
    expect(post('/delivery-targets/resolve')[1].data).toBe(original)
    expect(post('/delivery-targets/resolve')[1].headers.get('If-Match')).toBe(
      first.headers.get('If-Match'),
    )
    expect(post('/delivery-targets/resolve')[1].headers.get('X-CSRF-Token')).toBe('c'.repeat(64))
  })
  it('keeps the original uncertain issuance body and ETag after conflict review and uses fresh CSRF for explicit retry', async () => {
    await ready()
    issueFails = true
    await submit()
    await click('Confirm')
    await until(() => expect(document.body.textContent).toContain('The result is uncertain.'))
    const first = post('/managed')[0]
    expect(document.body.textContent).not.toContain('private upstream secret')
    policyReview = 'd'.repeat(64)
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'c'.repeat(64) }),
    )
    await until(() =>
      expect(
        [...document.querySelectorAll('button')].some(
          (x) => x.textContent === 'Retry original request' && !x.disabled,
        ),
      ).toBe(true),
    )
    await click('Read current facts')
    await until(() =>
      expect(document.body.textContent).toContain(
        'original uncertain request and review token are unchanged',
      ),
    )
    issueFails = false
    await click('Retry original request')
    await click('Confirm')
    await until(() => expect(post('/managed')).toHaveLength(2))
    expect(post('/managed')[1].data).toBe(first.data)
    expect(post('/managed')[1].headers.get('If-Match')).toBe('"' + review + '"')
    expect(post('/managed')[1].headers.get('X-CSRF-Token')).toBe('c'.repeat(64))
  })
  it.each(['cancel', 'unmount'] as const)(
    'rejects a retained Confirm callback after %s',
    async (mode) => {
      await ready()
      await submit()
      const confirm = capturedClick(button('Confirm'))
      if (mode === 'cancel') await click('Cancel')
      else await act(async () => root.render(null))
      await act(async () => confirm())
      expect(post('/managed')).toHaveLength(0)
    },
  )
  it('settles uncertainty only from the exact confirmed receipt after explicit review', async () => {
    await ready()
    issueFails = true
    await submit()
    await click('Confirm')
    await until(() => expect(document.body.textContent).toContain('The result is uncertain.'))
    result(post('/managed')[0], 'confirmed')
    await click('Read current receipt')
    await click('Read current receipt')
    await until(() =>
      expect(document.body.textContent).toContain('Confirmed for the local runtime'),
    )
    expect(received).not.toHaveBeenCalled()
    await click('Use confirmed receipt')
    expect(received).toHaveBeenCalledOnce()
    expect(post('/managed')).toHaveLength(1)
  })
  it.each([
    ['personal', false],
    ['personal', true],
    ['project', false],
    ['project', true],
  ] as const)(
    'recovers uncertain %s issuance (rotation=%s) through the exact request before knowing its Key',
    async (kind, rotating) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      const predecessor = rotating
        ? { ...key(), id: key().id.slice(0, -1) + '4', status: 'disabled' as const }
        : undefined
      await ready(predecessor)
      issueFails = true
      await submit()
      await click(en.confirm)
      await until(() => expect(document.body.textContent).toContain(en.uncertain))
      const suffix = rotating ? '/managed-rotation' : '/managed'
      const original = post(suffix)[0]
      result(original)
      const returnedKey = issuedReceipt!.key_id
      const originalRequest = issuedReceipt!.request_id
      expect(returnedKey).not.toBe(predecessor?.id)
      await click(en.readReceipt)
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.delivery_acknowledged))
      expect(requests.at(-1)!.url).toBe(base() + '/delivery-receipts/' + originalRequest)
      expect(received).not.toHaveBeenCalled()
      await fill('reconcile_reason', 'Recover the acknowledged original issuance')
      await click(en.reviewReconcile)
      await click(en.confirm)
      await until(() => expect(document.body.textContent).toContain(en.runtime_confirmed))
      expect(post('/delivery-reconcile')).toHaveLength(1)
      const recovery = post('/delivery-reconcile')[0]
      expect(recovery.url).toBe(base() + '/' + returnedKey + '/delivery-reconcile')
      expect(JSON.parse(recovery.data)).toEqual({
        request_id: originalRequest,
        reason: 'Recover the acknowledged original issuance',
      })
      expect(received).not.toHaveBeenCalled()
      await click(en.useReceipt)
      expect(received).toHaveBeenCalledOnce()
      expect(received.mock.calls[0][0]).toMatchObject({
        key_id: returnedKey,
        request_id: originalRequest,
        runtime_status: 'confirmed',
        replacement_call_status: rotating ? 'not_recorded' : 'not_applicable',
      })
      expect(post(suffix)).toHaveLength(1)
      expect(
        requests.some(
          (request) => request.method === 'delete' || request.url?.endsWith('/complete-rotation'),
        ),
      ).toBe(false)
    },
  )
  it('rejects late issuance and duplicate dispatch when the Session renews during I/O', async () => {
    await ready()
    const h = hold(base() + '/managed')
    await submit()
    await click('Confirm')
    await until(() => expect(post('/managed')).toHaveLength(1))
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'e'.repeat(64) }),
    )
    expect(post('/managed')[0].signal?.aborted).toBe(true)
    h.release()
    await until(() =>
      expect(
        [...document.querySelectorAll('button')].some(
          (x) => x.textContent === 'Retry original request',
        ),
      ).toBe(true),
    )
    expect(received).not.toHaveBeenCalled()
    expect(post('/managed')).toHaveLength(1)
  })
  it('discards pending drafts on actor change and never resurrects obsolete results', async () => {
    await ready()
    const h = hold(base() + '/managed')
    await submit()
    await click('Confirm')
    await until(() => expect(post('/managed')).toHaveLength(1))
    await act(async () =>
      cache.setQueryData(['auth', 'session'], {
        ...session(),
        user: { ...session().user, id: 'usr_01m36yee4gkbns18pfcqqc75a4' },
        csrf_token: 'c'.repeat(64),
      }),
    )
    h.release()
    await until(() =>
      expect(document.querySelector<HTMLInputElement>('input[name="subject"]')?.value).toBe(''),
    )
    expect(received).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('Retry original request')
  })
  it.each(['personal', 'project'] as const)(
    'rotates %s managed Keys with only server-owned metadata and retains the predecessor',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      const predecessor = {
        ...key(),
        id: (kind === 'project' ? 'pky' : 'key') + '_01m36yee4gkbns18pfcqqc75a4',
        status: 'disabled' as const,
      }
      await ready(predecessor)
      expect(document.querySelector('input[name="name"]')).toBeNull()
      expect(document.body.textContent).toContain('Disabled')
      await submit()
      await click('Confirm')
      await until(() => expect(received).toHaveBeenCalledOnce())
      const request = post('/managed-rotation')[0]
      expect(request.url).toBe(base() + '/' + predecessor.id + '/managed-rotation')
      expect(JSON.parse(request.data)).toMatchObject({ name: '', model_ids: [], expires_at: null })
      expect(predecessor.status).toBe('disabled')
      expect(
        requests.some(
          (x) =>
            x.url?.endsWith('/complete-rotation') ||
            x.url?.endsWith('/confirm') ||
            x.method === 'delete',
        ),
      ).toBe(false)
    },
  )
  it('updates existing validation and inline labels on language switch without losing input', async () => {
    await render()
    await until(() => expect(document.querySelector('input[name="name"]')).not.toBeNull())
    await fill('name', 'Draft')
    await submit()
    expect(document.body.textContent).toContain('Select at least one model.')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('请至少选择一个模型。')
    expect(document.body.textContent).toContain('交付方式')
    expect(document.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe('Draft')
    expect(post('/managed')).toHaveLength(0)
  })
  it('hides private facts and refuses retained dispatch during a renewed list read', async () => {
    await ready()
    await submit()
    const confirm = capturedClick(button('Confirm'))
    await act(async () => cache.invalidateQueries({ queryKey: ['keys'], refetchType: 'none' }))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => confirm())
    expect(post('/managed')).toHaveLength(0)
  })
  it('does not infer managed eligibility from a verified dependency when policy permits manual only', async () => {
    contextModes = ['manual']
    await render()
    await until(() =>
      expect(document.querySelector('select[aria-label="Delivery method"]')).not.toBeNull(),
    )
    expect([...document.querySelectorAll('option')].some((x) => x.value === 'managed_vault')).toBe(
      false,
    )
    await fill('name', 'Manual')
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[name="model_ids"]')!.click(),
    )
    await submit()
    expect(manual).toHaveBeenCalledOnce()
    expect(post('/managed')).toHaveLength(0)
  })
})
describe('owner delivery receipt', () => {
  it.each(['personal', 'project'] as const)(
    'renders and explicitly copies %s JWT Agent configuration without changing delivery facts',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      await render({ receipt: true })
      expect(document.querySelector('textarea')).toBeNull()
      await click(en.readReceipt)
      await until(() => expect(document.querySelector('textarea')).not.toBeNull())
      const textarea = document.querySelector('textarea')!
      const configuration = textarea.value
      expect(textarea.readOnly).toBe(true)
      expect(configuration).toContain('method "jwt"')
      expect(configuration).toContain('auth/jwt')
      expect(configuration).toContain(issuedReceipt!.descriptor!.path + '?version=1')
      expect(configuration).toContain('/var/run/secrets/routex/api-keys/' + id('kds'))
      expect(document.body.textContent).toContain(en.configurationBoundary)
      expect(document.querySelector('details')!.open).toBe(false)
      expect(writeText).not.toHaveBeenCalled()
      await click(en.copyConfiguration)
      expect(writeText).toHaveBeenCalledExactlyOnceWith(configuration)
      expect(document.body.textContent).toContain(en.configurationCopied)
      await act(async () => i18n.changeLanguage('zh'))
      expect(document.body.textContent).toContain(zh.applicationConfiguration)
      expect(document.body.textContent).toContain(zh.configurationCopied)
      expect(document.querySelector('textarea')!.value).toBe(configuration)
      await click(zh.copyDescriptor)
      expect(writeText.mock.calls[1][0]).toBe(JSON.stringify(issuedReceipt!.descriptor, null, 2))
      expect(document.body.textContent).toContain(zh.copied)
      expect(requests).toHaveLength(1)
      expect(requests[0].method).toBe('get')
      expect(caches()).not.toContain('workload.jwt')
      expect(caches()).not.toContain(configuration)
      expect(localStorage.getItem('routex.key')).toBeNull()
      expect(sessionStorage.length).toBe(0)
      expect(changed).not.toHaveBeenCalled()
    },
  )
  it('does not emit executable configuration for an inconsistent descriptor tuple', async () => {
    result(
      {
        data: JSON.stringify({ request_id: requestId }),
        url: base() + '/managed',
      } as InternalAxiosRequestConfig,
      'confirmed',
    )
    issuedReceipt!.descriptor!.path = 'api-keys/exact'
    await render({ receipt: true })
    await click(en.readReceipt)
    await until(() => expect(document.body.textContent).toContain(en.configurationUnavailable))
    expect(document.querySelector('textarea')).toBeNull()
    expect(document.body.textContent).not.toContain(en.copyConfiguration)
    expect(document.querySelector('details')!.textContent).toContain('api-keys/exact')
    await click(en.copyDescriptor)
    expect(writeText.mock.calls[0][0]).toContain('api-keys/exact')
    expect(requests).toHaveLength(1)
  })
  it('reports clipboard failure locally and permits an explicit configuration retry', async () => {
    result(
      {
        data: JSON.stringify({ request_id: requestId }),
        url: base() + '/managed',
      } as InternalAxiosRequestConfig,
      'confirmed',
    )
    await render({ receipt: true })
    await click(en.readReceipt)
    await until(() => expect(document.querySelector('textarea')).not.toBeNull())
    writeText.mockRejectedValueOnce(new Error('Clipboard blocked'))
    await click(en.copyConfiguration)
    expect(document.body.textContent).toContain(en.configurationCopyFailed)
    expect(document.body.textContent).not.toContain(en.configurationCopied)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain(zh.configurationCopyFailed)
    await click(zh.copyConfiguration)
    expect(writeText.mock.calls[0][0]).toBe(writeText.mock.calls[1][0])
    expect(document.body.textContent).toContain(zh.configurationCopied)
    expect(requests).toHaveLength(1)
    expect(changed).not.toHaveBeenCalled()
  })
  it.each(
    (['personal', 'project'] as const).flatMap((kind) =>
      (['configuration', 'descriptor'] as const).flatMap((mode) =>
        (['success', 'failure'] as const).flatMap((outcome) =>
          (['receipt', 'descriptor'] as const).map((source) => ({ kind, mode, outcome, source })),
        ),
      ),
    ),
  )(
    'rejects retained $kind $mode copy after same-authority $source refresh $outcome',
    async ({ kind, mode, outcome, source }) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      await render({ receipt: true, descriptorOnly: source === 'descriptor' })
      await click(en.readReceipt)
      await until(() => expect(document.querySelector('textarea')).not.toBeNull())
      const label = mode === 'configuration' ? en.copyConfiguration : en.copyDescriptor
      const retained = capturedClick(button(label))
      const original =
        mode === 'configuration'
          ? document.querySelector('textarea')!.value
          : JSON.stringify(issuedReceipt!.descriptor, null, 2)
      const refresh = hold(
        source === 'descriptor'
          ? base() + '/' + key().id + '/delivery-descriptor'
          : base() + '/delivery-receipts/' + requestId,
      )
      const read = capturedClick(button(en.readReceipt))
      await act(async () => {
        read()
        retained()
      })
      expect(writeText).not.toHaveBeenCalled()
      expect(document.querySelector('textarea')).toBeNull()
      receiptMissing = outcome === 'failure'
      await act(async () => refresh.release())
      await until(() => {
        if (outcome === 'failure')
          expect(document.body.textContent).toContain(en.receiptUnavailable)
        else expect(document.querySelector('textarea')).not.toBeNull()
      })
      await act(async () => retained())
      expect(writeText).not.toHaveBeenCalled()
      expect(document.body.textContent).not.toContain(en.configurationCopied)
      expect(document.body.textContent).not.toContain(en.copied)
      if (outcome === 'failure') {
        held = null
        receiptMissing = false
        await click(en.readReceipt)
        await until(() => expect(document.querySelector('textarea')).not.toBeNull())
        await act(async () => retained())
        expect(writeText).not.toHaveBeenCalled()
      }
      // An identical fresh server fact does not resurrect the original callback.
      await click(label)
      expect(writeText).toHaveBeenCalledExactlyOnceWith(original)
      expect(requests.every((request) => request.method === 'get')).toBe(true)
      expect(changed).not.toHaveBeenCalled()
    },
  )
  it.each(
    (['personal', 'project'] as const).flatMap((kind) =>
      (
        ['renewal', 'actor', 'target', 'unmount', 'error', 'missing', 'permission', 'list'] as const
      ).map((change) => ({ kind, change })),
    ),
  )(
    'rejects retained $kind copy dispatch and late clipboard notice after $change',
    async ({ kind, change }) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      await render({ receipt: true })
      await click(en.readReceipt)
      await until(() => expect(document.querySelector('textarea')).not.toBeNull())
      const copied = capturedClick(button(en.copyConfiguration))
      let release!: () => void
      writeText.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            release = resolve
          }),
      )
      await click(en.copyConfiguration)
      expect(button(en.copyConfiguration).disabled).toBe(true)
      await act(async () => copied())
      expect(writeText).toHaveBeenCalledOnce()
      if (change === 'renewal' || change === 'actor') {
        await act(async () =>
          cache.setQueryData(['auth', 'session'], {
            ...session(),
            user:
              change === 'actor'
                ? { ...session().user, id: 'usr_01m36yee4gkbns18pfcqqc75a4' }
                : session().user,
            csrf_token: 'c'.repeat(64),
          }),
        )
      } else if (change === 'target') {
        await render({
          receipt: true,
          receiptKeyId: key().id.slice(0, -1) + '4',
          receiptRequestId: '20000000-0000-4000-8000-000000000001',
        })
      } else if (change === 'error') {
        await act(async () =>
          cache
            .getQueryCache()
            .find({ queryKey: ['auth', 'session'], exact: true })!
            .setState({ status: 'error', error: new Error('Session unavailable') }),
        )
      } else if (change === 'missing') {
        await act(async () => cache.setQueryData(['auth', 'session'], null))
      } else if (change === 'permission') {
        await render({ receipt: true, allowed: false })
      } else if (change === 'list') {
        await act(async () =>
          cache.invalidateQueries({
            queryKey: scope.kind === 'personal' ? ['keys'] : ['project-keys'],
            refetchType: 'none',
          }),
        )
      } else {
        await act(async () => root.render(null))
      }
      expect(document.querySelector('textarea')).toBeNull()
      await act(async () => copied())
      expect(writeText).toHaveBeenCalledOnce()
      await act(async () => release())
      expect(document.body.textContent).not.toContain(en.configurationCopied)
      expect(changed).not.toHaveBeenCalled()
      expect(caches()).not.toContain('workload.jwt')
    },
  )
  it('keeps a missing receipt uncertain and never automatically reads, retries or copies a secret', async () => {
    receiptMissing = true
    await render({ receipt: true })
    expect(requests).toHaveLength(0)
    await click('Read current receipt')
    await until(() =>
      expect(document.body.textContent).toContain('Absence does not prove that no write occurred'),
    )
    expect(requests).toHaveLength(1)
    expect(requests[0].method).toBe('get')
    expect(document.body.textContent).not.toContain('Write acknowledged')
    expect(changed).not.toHaveBeenCalled()
  })
  it('reconciles only the original acknowledged intent with exact receipt ETag and no Writer replay', async () => {
    const fake = {
      data: JSON.stringify({ request_id: requestId }),
      url: '/keys/managed',
    } as InternalAxiosRequestConfig
    result(fake)
    await render({ receipt: true })
    await click('Read current receipt')
    await until(() => expect(document.body.textContent).toContain('Write acknowledged'))
    await fill('reconcile_reason', 'Recover local publication')
    reconcileFails = true
    await click('Review local publication recovery')
    await click('Confirm')
    await until(() =>
      expect(document.body.textContent).toContain('Local publication recovery is uncertain'),
    )
    const first = post('/delivery-reconcile')[0]
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'c'.repeat(64) }),
    )
    await click('Read current receipt')
    await until(() => expect(document.body.textContent).toContain('Write acknowledged'))
    reconcileFails = false
    await click('Retry original request')
    await click('Confirm')
    await until(() => expect(changed).toHaveBeenCalledOnce())
    expect(post('/delivery-reconcile')).toHaveLength(2)
    expect(post('/delivery-reconcile')[1].data).toBe(first.data)
    expect(post('/delivery-reconcile')[1].headers.get('If-Match')).toBe(
      first.headers.get('If-Match'),
    )
    expect(post('/delivery-reconcile')[1].headers.get('X-CSRF-Token')).toBe('c'.repeat(64))
    expect(post('/managed')).toHaveLength(0)
    expect(document.body.textContent).toContain('Coordinator: Not configured')
    expect(document.body.textContent).toContain('9007199254740993')
    expect(caches()).not.toContain('Recover local publication')
  })
  it.each(['personal', 'project'] as const)(
    'shows only server replacement-call facts for %s receipts in both languages',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      await render({ receipt: true })
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      for (const status of ['not_applicable', 'not_recorded', 'completed'] as const) {
        issuedReceipt = { ...issuedReceipt!, replacement_call_status: status }
        await click(en.readReceipt)
        await until(() =>
          expect(document.body.textContent).toContain(
            en[('replacementCall_' + status) as keyof typeof en],
          ),
        )
        expect(document.body.textContent).toContain(en.replacementCallStatus)
        expect(document.body.textContent).toContain(en.replacementCallBoundary)
        expect(document.body.textContent).toContain(
          'Coordinator: Not configured. Application status: Unknown.',
        )
        await act(async () => i18n.changeLanguage('zh'))
        expect(document.body.textContent).toContain(zh.replacementCallStatus)
        expect(document.body.textContent).toContain(
          zh[('replacementCall_' + status) as keyof typeof zh],
        )
        expect(document.body.textContent).toContain(zh.replacementCallBoundary)
        await act(async () => i18n.changeLanguage('en'))
      }
      expect(requests).toHaveLength(3)
      expect(requests.every((request) => request.method === 'get')).toBe(true)
      expect(changed).not.toHaveBeenCalled()
      expect(received).not.toHaveBeenCalled()
    },
  )
  it.each(['personal', 'project'] as const)(
    'clears %s replacement-call facts during refresh, read failure and Session renewal',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      issuedReceipt = { ...issuedReceipt!, replacement_call_status: 'completed' }
      await render({ receipt: true })
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.replacementCall_completed))
      expect(document.querySelector('textarea')).not.toBeNull()
      const refresh = hold(base() + '/delivery-receipts/' + requestId)
      await click(en.readReceipt)
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      expect(document.querySelector('textarea')).toBeNull()
      receiptMissing = true
      await act(async () => refresh.release())
      await until(() => expect(document.body.textContent).toContain(en.receiptUnavailable))
      expect(document.querySelector('textarea')).toBeNull()
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      receiptMissing = false
      held = null
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.replacementCall_completed))
      const obsoleteRead = capturedClick(button(en.readReceipt))
      const renewal = hold(base() + '/delivery-receipts/' + requestId)
      await click(en.readReceipt)
      const request = requests.at(-1)!
      await act(async () =>
        cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'e'.repeat(64) }),
      )
      expect(request.signal?.aborted).toBe(true)
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      const count = requests.length
      await act(async () => obsoleteRead())
      expect(requests).toHaveLength(count)
      await act(async () => renewal.release())
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      held = null
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.replacementCall_completed))
      await act(async () =>
        cache
          .getQueryCache()
          .find({ queryKey: ['auth', 'session'], exact: true })!
          .setState({ status: 'error', error: new Error('Session unavailable') }),
      )
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      await act(async () =>
        cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'f'.repeat(64) }),
      )
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      expect(changed).not.toHaveBeenCalled()
    },
  )
  it.each(['personal', 'project'] as const)(
    'discards %s receipt evidence and rejects late replies after an actor change',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      issuedReceipt = { ...issuedReceipt!, replacement_call_status: 'completed' }
      await render({ receipt: true })
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.replacementCall_completed))
      const read = hold(base() + '/delivery-receipts/' + requestId)
      await click(en.readReceipt)
      const request = requests.at(-1)!
      await act(async () =>
        cache.setQueryData(['auth', 'session'], {
          ...session(),
          user: { ...session().user, id: 'usr_01m36yee4gkbns18pfcqqc75a4' },
          csrf_token: 'c'.repeat(64),
        }),
      )
      expect(request.signal?.aborted).toBe(true)
      await act(async () => read.release())
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      expect(changed).not.toHaveBeenCalled()
    },
  )
  it.each(['personal', 'project'] as const)(
    'binds %s receipt facts to the reviewed Key and request across target changes',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result(
        {
          data: JSON.stringify({ request_id: requestId }),
          url: base() + '/managed',
        } as InternalAxiosRequestConfig,
        'confirmed',
      )
      issuedReceipt = { ...issuedReceipt!, replacement_call_status: 'completed' }
      await render({ receipt: true })
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.replacementCall_completed))
      const obsoleteRead = capturedClick(button(en.readReceipt))
      const read = hold(base() + '/delivery-receipts/' + requestId)
      await click(en.readReceipt)
      const request = requests.at(-1)!
      const nextKey = key().id.slice(0, -1) + '4'
      const nextRequest = '20000000-0000-4000-8000-000000000001'
      await render({ receipt: true, receiptKeyId: nextKey, receiptRequestId: nextRequest })
      expect(request.signal?.aborted).toBe(true)
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      const count = requests.length
      await act(async () => obsoleteRead())
      expect(requests).toHaveLength(count)
      await act(async () => read.release())
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      held = null
      issuedReceipt = {
        ...issuedReceipt!,
        request_id: nextRequest,
        key_id: nextKey,
        replacement_call_status: 'not_recorded',
        descriptor: { ...issuedReceipt!.descriptor!, key_id: nextKey },
      }
      await click(en.readReceipt)
      await until(() =>
        expect(document.body.textContent).toContain(en.replacementCall_not_recorded),
      )
      expect(requests.at(-1)!.url).toBe(base() + '/delivery-receipts/' + nextRequest)
      issuedReceipt = {
        ...issuedReceipt,
        key_id: key().id,
        replacement_call_status: 'completed',
        descriptor: { ...issuedReceipt.descriptor!, key_id: key().id },
      }
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.receiptUnavailable))
      expect(document.body.textContent).not.toContain(en.replacementCallStatus)
      expect(document.body.textContent).not.toContain(en.replacementCall_completed)
      expect(document.body.textContent).not.toContain('api-keys/exact')
      expect(changed).not.toHaveBeenCalled()
    },
  )

  it.each(['personal', 'project'] as const)(
    'discards %s uncertain reconciliation on target change and rejects queued old review',
    async (kind) => {
      if (kind === 'project') scope = { kind: 'project', projectId: id('prj') }
      result({
        data: JSON.stringify({ request_id: requestId }),
        url: base() + '/managed',
      } as InternalAxiosRequestConfig)
      await render({ receipt: true })
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.delivery_acknowledged))
      await fill('reconcile_reason', 'Original publication recovery')
      const obsoleteReview = capturedClick(button(en.reviewReconcile))
      reconcileFails = true
      await click(en.reviewReconcile)
      await click(en.confirm)
      await until(() => expect(document.body.textContent).toContain(en.reconcileFailed))
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.retryOriginal))
      const nextKey = key().id.slice(0, -1) + '4'
      const nextRequest = '20000000-0000-4000-8000-000000000001'
      await render({ receipt: true, receiptKeyId: nextKey, receiptRequestId: nextRequest })
      expect(document.body.textContent).not.toContain(en.retryOriginal)
      await act(async () => obsoleteReview())
      expect(document.body.textContent).not.toContain(en.confirmReconcile)
      expect(post('/delivery-reconcile')).toHaveLength(1)
      issuedReceipt = { ...issuedReceipt!, key_id: nextKey, request_id: nextRequest }
      await click(en.readReceipt)
      await until(() => expect(document.body.textContent).toContain(en.reviewReconcile))
      const reason = document.querySelector<HTMLInputElement>('input[name="reconcile_reason"]')!
      expect(reason.value).toBe('')
      expect(reason.disabled).toBe(false)
      await fill('reconcile_reason', 'Review the new target separately')
      await click(en.reviewReconcile)
      await click(en.confirm)
      await until(() => expect(post('/delivery-reconcile')).toHaveLength(2))
      const latest = post('/delivery-reconcile')[1]
      expect(latest.url).toBe(base() + '/' + nextKey + '/delivery-reconcile')
      expect(JSON.parse(latest.data)).toEqual({
        request_id: nextRequest,
        reason: 'Review the new target separately',
      })
      expect(changed).not.toHaveBeenCalled()
    },
  )
})
