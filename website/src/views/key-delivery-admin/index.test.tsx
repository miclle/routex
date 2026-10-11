import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/keyDelivery'
import zh from '@/i18n/locales/zh/keyDelivery'
import type { Session } from '@/types/auth'
import type {
  KeyCallerTemplate,
  KeyDeliveryPolicy,
  KeyDeliverySecurityTuple,
  KeyDeliveryTargetReview,
} from '@/types/key-delivery'
import { KeyDeliveryError } from '@/api/key-delivery'
import Workspace, { type KeyDeliveryAdminWorkspaceProps } from './index'
import { useDeliveryLifetime } from './lifetime'
const api = vi.hoisted(() => ({
  getKeyDeliveryPolicy: vi.fn(),
  getKeyCallerTemplate: vi.fn(),
  getKeyDeliveryTargets: vi.fn(),
  reviewKeyDeliveryTarget: vi.fn(),
  saveKeyDeliveryPolicy: vi.fn(),
  saveKeyDeliveryIdentity: vi.fn(),
  saveKeyCallerTemplate: vi.fn(),
  updateKeyDeliveryTarget: vi.fn(),
  verifyKeyDelivery: vi.fn(),
}))
vi.mock('@/api/key-delivery', () => ({
  ...api,
  KeyDeliveryError: class extends Error {
    constructor(public status: number) {
      super('Request rejected')
    }
  },
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const hash = 'a'.repeat(64),
  nextHash = 'b'.repeat(64)
const vault = 'vlt_01arz3ndektsv4rrffq69g5fav',
  profile = 'vap_01arz3ndektsv4rrffq69g5fav'
const writerIdentity = 'kda_01arz3ndektsv4rrffq69g5fav',
  provisionerIdentity = 'kda_01arz3ndektsv4rrffq69g5faw'
let root: Root, host: HTMLDivElement, cache: QueryClient, props: KeyDeliveryAdminWorkspaceProps
let policy: KeyDeliveryPolicy, template: KeyCallerTemplate, target: KeyDeliveryTargetReview
let releases: (() => void)[]
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  releases.push(release)
  return { promise, release }
}
function session(csrf = 'a'.repeat(64), actor = 'usr_alice'): Session {
  return {
    user: { id: actor, name: 'Alice', email: 'alice@example.test', role: 'admin' },
    csrf_token: csrf,
  }
}
async function flush() {
  for (let turn = 0; turn < 8; turn++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Workspace {...props} />
      </QueryClientProvider>,
    ),
  )
  await flush()
}
function button(label: string, within: ParentNode = host): HTMLButtonElement {
  const found = [...within.querySelectorAll('button')].find((item) => item.textContent === label)
  if (!found) throw new Error(`Missing button: ${label}`)
  return found
}
async function click(label: string, within: ParentNode = host) {
  await act(async () => button(label, within).click())
  await flush()
}
async function fill(element: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    const prototype =
      element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function policyForm() {
  return button('Review policy changes').closest('form')!
}
async function preparePolicy() {
  await fill(policyForm().querySelector('textarea')!, 'Reviewed delivery policy')
  await click('Review policy changes')
}
function dialog() {
  return document.querySelector('[role="dialog"]')!
}
function credentials() {
  return [...host.querySelectorAll('form')].filter((form) =>
    form.querySelector<HTMLInputElement>('input[type="password"]'),
  )
}
beforeEach(async () => {
  vi.resetAllMocks()
  releases = []
  i18n.addResourceBundle('en', 'keyDelivery', en, true, true)
  i18n.addResourceBundle('zh', 'keyDelivery', zh, true, true)
  await i18n.changeLanguage('en')
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(
    ['permissions', 'usr_alice', 'delivery'],
    ['secrets.read', 'secrets.write', 'secrets.test'],
  )
  props = {
    actor: 'usr_alice',
    admin: true,
    fresh: true,
    generation: 1,
    permissionKey: ['permissions', 'usr_alice', 'delivery'],
    vaultChoices: [
      {
        id: vault,
        name: 'Delivery Vault',
        endpoint: 'https://vault.example.test',
        namespace: 'private-space',
        mount: 'keys',
      },
    ],
    choicesCurrent: true,
    choicesGeneration: 1,
  }
  policy = {
    allowed_modes: ['manual'],
    default_mode: 'manual',
    managed_available: false,
    failure_code: 'not_configured',
    review_etag: hash,
    integration_id: vault,
    writer_identity_id: writerIdentity,
    provisioner_identity_id: provisionerIdentity,
    template_configured: true,
  }
  template = {
    auth_mount: '',
    issuer: '',
    jwks_url: '',
    audience: '',
    allowed_subjects: [],
    token_ttl_seconds: 0,
    token_max_ttl_seconds: 0,
    verified: false,
    review_etag: hash,
  }
  const tuple: KeyDeliverySecurityTuple = {
    profile_id: profile,
    owner_kind: 'personal',
    owner_id: 'usr_alice',
    owner_birth: '2026-10-11T00:00:00Z',
    owner_digest: hash,
    consumer_id: 'orders',
    environment: 'production',
    profile_config_revision: hash,
    profile_security_revision: hash,
    integration_id: vault,
    integration_birth: '2026-10-11T00:00:00Z',
    integration_revision: hash,
    vault_revision_id: 'vlr_revision',
    vault_revision_birth: '2026-10-11T00:00:00Z',
    infrastructure_security_revision: hash,
    writer_identity_id: 'kda_writer',
    writer_identity_birth: '2026-10-11T00:00:00Z',
    writer_generation: 'kdg_writer',
    provisioner_identity_id: 'kda_provisioner',
    provisioner_identity_birth: '2026-10-11T00:00:00Z',
    provisioner_generation: 'kdg_provisioner',
    template_id: 'jwt',
    template_birth: '2026-10-11T00:00:00Z',
    template_security_revision: hash,
    auth_mount: 'jwt',
    issuer: 'https://issuer.example.test',
    jwks_url: 'https://issuer.example.test/keys',
    audience: 'orders',
    trust_security_revision: hash,
    profile_birth: '2026-10-11T00:00:00Z',
    principal_id: 'vpb_principal',
    principal_birth: '2026-10-11T00:00:00Z',
    principal_security_revision: hash,
    method: 'jwt',
    subject: 'orders-exact',
  }
  target = {
    id: profile,
    consumer_id: 'orders',
    application_name: 'Orders',
    environment: 'production',
    status: 'pending',
    control_verified: false,
    isolation: { status: 'missing', revision: null, recorded_at: null },
    failure_code: null,
    review_etag: hash,
    security_tuple: tuple,
  }
  api.getKeyDeliveryPolicy.mockImplementation(async () => structuredClone(policy))
  api.getKeyCallerTemplate.mockImplementation(async () => structuredClone(template))
  api.getKeyDeliveryTargets.mockResolvedValue({
    items: [
      { ...target, security_tuple: undefined, owner_kind: 'personal', owner_id: 'usr_alice' },
    ],
    next_cursor: null,
  })
  api.reviewKeyDeliveryTarget.mockImplementation(async () => structuredClone(target))
  api.saveKeyDeliveryPolicy.mockResolvedValue(policy)
  api.saveKeyDeliveryIdentity.mockResolvedValue({
    id: 'kda_saved',
    purpose: 'writer',
    configured: true,
    review_etag: nextHash,
  })
  api.saveKeyCallerTemplate.mockResolvedValue(template)
  api.updateKeyDeliveryTarget.mockResolvedValue(target)
  api.verifyKeyDelivery.mockResolvedValue(policy)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  releases.forEach((release) => release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  vi.useRealTimers()
  await i18n.changeLanguage('en')
})
describe('API-Key delivery administrative workspace', () => {
  it('keeps the overview, Profile queue and mode cards bilingual without inferred readiness', async () => {
    await render()
    const table = host.querySelector<HTMLTableElement>(
      'table[aria-label="Application targets and Profiles"]',
    )!
    expect(table).not.toBeNull()
    expect(table.tBodies[0].rows).toHaveLength(1)
    expect(table.tBodies[0].rows[0].cells[0].textContent).toContain('Orders')
    expect(table.tBodies[0].rows[0].cells[0].textContent).toContain('production')
    expect(button('Review Profile', table).disabled).toBe(false)
    expect(host.textContent).toContain('Managed delivery status')
    expect(host.textContent).toContain('Unavailable')
    expect(host.textContent).toContain('Not configured')
    expect(host.textContent).not.toContain('Mark READY')
    await fill(policyForm().querySelector('textarea')!, 'Preserved draft')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('API-Key 交付策略')
    expect(host.textContent).toContain('批准')
    expect(
      [...host.querySelectorAll('textarea')].some((input) => input.value === 'Preserved draft'),
    ).toBe(true)
    expect(api.saveKeyDeliveryPolicy).not.toHaveBeenCalled()
  })
  it('configures both identities before linking an initially unconfigured policy', async () => {
    policy = {
      ...policy,
      integration_id: '',
      writer_identity_id: '',
      provisioner_identity_id: '',
      template_configured: false,
    }
    api.saveKeyDeliveryIdentity.mockImplementation(async (input) => ({
      id: input.purpose === 'writer' ? writerIdentity : provisionerIdentity,
      purpose: input.purpose,
      configured: true,
      review_etag: nextHash,
    }))
    await render()
    for (const [index, purpose] of ['writer', 'provisioner'].entries()) {
      const form = credentials()[index]
      const select = form.querySelector('select')!
      await act(async () => {
        select.value = vault
        select.dispatchEvent(new Event('change', { bubbles: true }))
      })
      await fill(
        form.querySelector<HTMLInputElement>('input[type="password"]')!,
        `private-${purpose}-token`,
      )
      await fill(form.querySelector('textarea')!, `Dedicated ${purpose}`)
      await click('Review identity credential', form)
      await click('Confirm operation', dialog())
    }
    expect(host.textContent).toContain(writerIdentity)
    expect(host.textContent).toContain(provisionerIdentity)
    expect(host.textContent).not.toContain('Original operation outcome unknown')
    expect(api.saveKeyDeliveryIdentity).toHaveBeenCalledTimes(2)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(api.saveKeyDeliveryPolicy).not.toHaveBeenCalled()
    const select = policyForm().querySelector('select')!
    await act(async () => {
      select.value = vault
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await click('Use saved API-Key Writer')
    await click('Use saved Vault Provisioner')
    expect(
      [...policyForm().querySelectorAll<HTMLInputElement>('input')]
        .filter((input) => input.type === 'text')
        .map((input) => input.value),
    ).toEqual([writerIdentity, provisionerIdentity])
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('private-writer-token')
  })
  it('requires explicit confirmation and preserves the reviewed policy through a conflict', async () => {
    api.saveKeyDeliveryPolicy.mockRejectedValue(new KeyDeliveryError(409))
    await render()
    await preparePolicy()
    expect(api.saveKeyDeliveryPolicy).not.toHaveBeenCalled()
    expect(dialog().textContent).toContain(hash)
    await click('Confirm operation', dialog())
    expect(api.saveKeyDeliveryPolicy).toHaveBeenCalledTimes(1)
    expect(api.saveKeyDeliveryPolicy.mock.calls[0].slice(1, 3)).toEqual([hash, 'a'.repeat(64)])
    expect(host.textContent).toContain('reviewed version changed')
    expect(policyForm().querySelector('textarea')!.value).toBe('Reviewed delivery policy')
  })
  it('retains an identical uncertain request through reads and another rejected retry', async () => {
    api.saveKeyDeliveryPolicy
      .mockRejectedValueOnce(new KeyDeliveryError(0))
      .mockRejectedValueOnce(new KeyDeliveryError(409))
    await render()
    await preparePolicy()
    await click('Confirm operation', dialog())
    const original = structuredClone(api.saveKeyDeliveryPolicy.mock.calls[0][0])
    policy = { ...policy, review_etag: nextHash }
    await click('Read current facts')
    await click('Retry original operation', dialog())
    expect(api.saveKeyDeliveryPolicy.mock.calls[1][0]).toEqual(original)
    expect(api.saveKeyDeliveryPolicy.mock.calls[1][1]).toBe(hash)
    expect(dialog().textContent).toContain('Original operation outcome unknown')
    expect(host.textContent).not.toContain('exact operation was confirmed')
  })
  it('keeps Tokens outside query and mutation caches and discards stale successful replies after renewal', async () => {
    const held = deferred()
    api.saveKeyDeliveryIdentity.mockImplementationOnce(async () => {
      await held.promise
      return { id: 'kda_stale', purpose: 'writer', configured: true, review_etag: nextHash }
    })
    await render()
    const form = credentials()[0]
    await fill(
      form.querySelector<HTMLInputElement>('input[type="password"]')!,
      'private-machine-token',
    )
    await fill(form.querySelector('textarea')!, 'Dedicated Writer')
    await click('Review identity credential', form)
    expect(form.querySelector<HTMLInputElement>('input[type="password"]')!.value).toBe('')
    expect(dialog().textContent).not.toContain('private-machine-token')
    await click('Confirm operation', dialog())
    await act(async () => cache.setQueryData(['auth', 'session'], session('b'.repeat(64))))
    await flush()
    await act(async () => held.release())
    await flush()
    expect(host.textContent).not.toContain('kda_stale')
    expect(host.textContent).not.toContain('exact operation was confirmed')
    expect(api.saveKeyDeliveryIdentity.mock.calls[0][3].aborted).toBe(true)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('private-machine-token')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    await click('Retry original operation', dialog())
    expect(api.saveKeyDeliveryIdentity.mock.calls[1][0]).toEqual(
      api.saveKeyDeliveryIdentity.mock.calls[0][0],
    )
    expect(api.saveKeyDeliveryIdentity.mock.calls[1][2]).toBe('b'.repeat(64))
  })
  it('retries an uncertain selection only after fresh matching Vault options, without changing its body', async () => {
    api.saveKeyDeliveryPolicy.mockRejectedValueOnce(new KeyDeliveryError(0))
    await render()
    await preparePolicy()
    await click('Confirm operation', dialog())
    const original = structuredClone(api.saveKeyDeliveryPolicy.mock.calls[0][0])
    props = { ...props, choicesCurrent: false, choicesGeneration: 2 }
    await render()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('https://vault.example.test')
    props = {
      ...props,
      choicesCurrent: true,
      vaultChoices: props.vaultChoices.map((choice) => ({ ...choice })),
    }
    await render()
    await click('Retry original operation', dialog())
    expect(api.saveKeyDeliveryPolicy.mock.calls[1][0]).toEqual(original)
    expect(api.saveKeyDeliveryPolicy.mock.calls[1][1]).toBe(hash)
  })
  it('does not install late state into another actor or retain Tokens after dismissal', async () => {
    const held = deferred()
    api.saveKeyDeliveryIdentity.mockImplementationOnce(async () => {
      await held.promise
      return { id: 'kda_old_actor', purpose: 'writer', configured: true, review_etag: nextHash }
    })
    await render()
    const form = credentials()[0]
    await fill(
      form.querySelector<HTMLInputElement>('input[type="password"]')!,
      'discard-this-token',
    )
    await fill(form.querySelector('textarea')!, 'Dedicated Writer')
    await click('Review identity credential', form)
    await click('Cancel', dialog())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(form.querySelector<HTMLInputElement>('input[type="password"]')!.value).toBe('')
    await fill(form.querySelector<HTMLInputElement>('input[type="password"]')!, 'old-actor-token')
    await click('Review identity credential', form)
    await click('Confirm operation', dialog())
    props = { ...props, actor: 'usr_bob', permissionKey: ['permissions', 'usr_bob', 'delivery'] }
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session('c'.repeat(64), 'usr_bob'))
      cache.setQueryData(props.permissionKey, ['secrets.read', 'secrets.write', 'secrets.test'])
    })
    await render()
    await act(async () => held.release())
    await flush()
    expect(host.textContent).not.toContain('kda_old_actor')
    expect(host.textContent).not.toContain('old-actor-token')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('keeps read and write/test authority independent and does not notify on observer bookkeeping', async () => {
    cache.setQueryData(props.permissionKey, ['secrets.read'])
    await render()
    expect(button('Verify dependencies').disabled).toBe(true)
    await fill(policyForm().querySelector('textarea')!, 'Reviewed delivery policy')
    expect(button('Review policy changes').disabled).toBe(true)
    await act(async () =>
      cache.setQueryData(props.permissionKey, ['secrets.read', 'secrets.write']),
    )
    await flush()
    await preparePolicy()
    const query = cache.getQueryCache().find({ queryKey: ['auth', 'session'] })!
    await act(async () =>
      cache
        .getQueryCache()
        .notify({ type: 'observerOptionsUpdated', query, observer: {} as never }),
    )
    expect(dialog()).not.toBeNull()
    await click('Cancel', dialog())
    function WriteOnly() {
      const life = useDeliveryLifetime(props, { current: true, generation: 1 })
      return (
        <button
          disabled={!life.current('secrets.write', true)}
          onClick={() =>
            life.prepare({
              kind: 'verify',
              etag: hash,
              input: { reason: 'Independent write and test' },
            })
          }
        >
          {life.intent ? 'Prepared' : 'Prepare'}
        </button>
      )
    }
    await act(async () => {
      cache.setQueryData(props.permissionKey, ['secrets.write', 'secrets.test'])
      root.render(
        <QueryClientProvider client={cache}>
          <WriteOnly />
        </QueryClientProvider>,
      )
    })
    // Settle the replaced workspace's gcTime: 0 queries before reviewing a
    // new intent; their real removal must still fence undispatched work.
    await flush()
    expect(
      cache.getQueryCache().findAll({ queryKey: ['key-delivery-admin', props.actor] }),
    ).toHaveLength(0)
    expect(cache.getQueryData(props.permissionKey)).toEqual(['secrets.write', 'secrets.test'])
    expect(button('Prepare').disabled).toBe(false)
    await click('Prepare')
    expect(host.textContent).toBe('Prepared')
    expect(api.verifyKeyDelivery).not.toHaveBeenCalled()
    await act(async () =>
      cache
        .getQueryCache()
        .notify({ type: 'observerOptionsUpdated', query, observer: {} as never }),
    )
    expect(host.textContent).toBe('Prepared')
    await act(async () => cache.setQueryData(['auth', 'session'], session('b'.repeat(64))))
    await flush()
    expect(host.textContent).toBe('Prepare')
    await click('Prepare')
    expect(host.textContent).toBe('Prepared')
    await act(async () => cache.removeQueries({ queryKey: props.permissionKey, exact: true }))
    await flush()
    expect(host.textContent).toBe('Prepare')
    expect(button('Prepare').disabled).toBe(true)
    expect(api.verifyKeyDelivery).not.toHaveBeenCalled()
  })
  it('separates Principal approval from exact-tuple isolation and reconciliation', async () => {
    await render()
    await click('Review Profile')
    expect(dialog().textContent).toContain('orders-exact')
    const first = dialog()
    await fill(first.querySelector('textarea')!, 'Independently verified Principal control')
    await click('Approve Principal control', first)
    const confirmation = [...document.querySelectorAll('[role="dialog"]')].at(-1)!
    expect(confirmation.textContent).toContain('Owner digest')
    await click('Confirm operation', confirmation)
    expect(api.updateKeyDeliveryTarget.mock.calls[0].slice(0, 4)).toEqual([
      profile,
      'approval',
      {
        approved: true,
        control_verified: true,
        reason: 'Independently verified Principal control',
      },
      hash,
    ])
    expect(
      api.updateKeyDeliveryTarget.mock.calls.some(
        (call) => call[1] === 'isolation' || call[1] === 'reconcile',
      ),
    ).toBe(false)
  })
  it('hides obsolete Vault choices and blocks both prepared selection and credential dispatch', async () => {
    await render()
    await preparePolicy()
    props = { ...props, choicesCurrent: false, choicesGeneration: 2 }
    await render()
    expect(host.textContent).not.toContain('https://vault.example.test')
    expect(button('Review policy changes').disabled).toBe(true)
    expect(credentials().every((form) => button('Review identity credential', form).disabled)).toBe(
      true,
    )
    expect(api.saveKeyDeliveryPolicy).not.toHaveBeenCalled()
    expect(api.saveKeyDeliveryIdentity).not.toHaveBeenCalled()
  })
  it('retains an unavailable configured Vault selection without clearing or authorizing it', async () => {
    const choices = props.vaultChoices
    props = { ...props, vaultChoices: [] }
    await render()
    const selectors = [
      policyForm().querySelector<HTMLSelectElement>('select')!,
      ...credentials().map((form) => form.querySelector<HTMLSelectElement>('select')!),
    ]
    expect(selectors).toHaveLength(3)
    for (const select of selectors) {
      expect(select.value).toBe(vault)
      expect(select.selectedOptions[0].disabled).toBe(true)
      expect(select.selectedOptions[0].textContent).toBe(`Unavailable Vault (${vault})`)
    }
    await fill(policyForm().querySelector('textarea')!, 'Retain exact configured Vault')
    expect(button('Review policy changes').disabled).toBe(true)
    expect(credentials().every((form) => button('Review identity credential', form).disabled)).toBe(
      true,
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    await flush()
    expect(host.textContent).toContain(`Vault 不可用（${vault}）`)
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    await flush()
    props = { ...props, vaultChoices: choices, choicesGeneration: 2 }
    await render()
    const restored = [
      policyForm().querySelector<HTMLSelectElement>('select')!,
      ...credentials().map((form) => form.querySelector<HTMLSelectElement>('select')!),
    ]
    expect(
      restored.every(
        (select) =>
          select.value === vault &&
          !select.selectedOptions[0].disabled &&
          select.selectedOptions[0].textContent === 'Delivery Vault',
      ),
    ).toBe(true)
    expect(api.saveKeyDeliveryPolicy).not.toHaveBeenCalled()
    expect(api.saveKeyDeliveryIdentity).not.toHaveBeenCalled()
  })
  it('expires transient confirmation material without dispatch or cached credentials', async () => {
    function Expiring() {
      const life = useDeliveryLifetime(props, { current: true, generation: 1 })
      return (
        <button
          onClick={() =>
            life.prepare({
              kind: 'identity',
              etag: hash,
              input: {
                purpose: 'writer',
                integration_id: vault,
                token: 'expires-without-dispatch',
                reason: 'Dedicated Writer',
              },
            })
          }
        >
          {life.intent ? 'Captured' : life.notice === 'expired' ? 'Expired' : 'Capture'}
        </button>
      )
    }
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Expiring />
        </QueryClientProvider>,
      ),
    )
    vi.useFakeTimers()
    await act(async () => button('Capture').click())
    expect(host.textContent).toBe('Captured')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_001)
    })
    expect(host.textContent).toBe('Expired')
    expect(api.saveKeyDeliveryIdentity).not.toHaveBeenCalled()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('expires-without-dispatch')
  })
  it('keeps translation keys and interpolation placeholders paired', () => {
    const flatten = (value: object, prefix = ''): Record<string, string> =>
      Object.fromEntries(
        Object.entries(value).flatMap(([key, item]) =>
          typeof item === 'string'
            ? [[prefix + key, item]]
            : Object.entries(flatten(item, prefix + key + '.')),
        ),
      )
    const english = flatten(en),
      chinese = flatten(zh)
    expect(Object.keys(english).sort()).toEqual(Object.keys(chinese).sort())
    for (const key of Object.keys(english))
      expect((english[key].match(/{{[^}]+}}/g) ?? []).sort()).toEqual(
        (chinese[key].match(/{{[^}]+}}/g) ?? []).sort(),
      )
    expect(Object.keys(en.tuple).sort()).toEqual(Object.keys(target.security_tuple).sort())
  })
})
