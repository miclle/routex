import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { DeliveryAuthority } from '@/views/key-delivery-admin/lifetime'
import ControllerWorkspace from './index'
const api = vi.hoisted(() => ({
  getControllerSlots: vi.fn(),
  getControllers: vi.fn(),
  getControllerReview: vi.fn(),
  getController: vi.fn(),
  enrollController: vi.fn(),
  assignController: vi.fn(),
  revokeController: vi.fn(),
  getControllerEnrollmentReceipt: vi.fn(),
}))
vi.mock('@/api/key-delivery-controller', () => ({
  ...api,
  validControllerReason: (value: string) => !!value && value.length <= 1000,
  DeliveryControllerError: class extends Error {
    constructor(public status: number) {
      super('Unavailable')
    }
  },
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const id = (prefix: string) => `${prefix}_01arz3ndektsv4rrffq69g5fav`,
  hash = 'a'.repeat(64),
  target = id('vap'),
  slot = id('kds'),
  controller = id('kcg'),
  birth = '2026-10-14T01:00:00Z'
const config = {
  schema_version: 1,
  slot_id: slot,
  descriptor_revision: '1',
  key_id: id('key'),
  configuration_digest: hash,
  agent_configuration: 'vault {\n  address = "https://vault.example.invalid"\n}\n',
  jwt_file: '/var/run/secrets/routex/workload.jwt',
  key_file: `/var/run/secrets/routex/api-keys/${slot}`,
}
const view = {
  id: controller,
  name: 'Independent controller',
  slot_id: slot,
  owner_kind: 'personal',
  owner_id: id('usr'),
  expires_at: '2026-10-15T01:00:00Z',
  revoked_at: null,
  assigned_descriptor_revision: '1',
  assigned_configuration_digest: hash,
  current_configuration: config,
  configuration_status: 'assigned',
  last_acknowledgement: {
    request_id: '20000000-0000-4000-8000-000000000001',
    descriptor_revision: '1',
    configuration_digest: hash,
    agent_state: 'unknown',
    output_state: 'written',
    recorded_at: birth,
    evidence_kind: 'controller_report',
  },
  review_etag: hash,
}
let root: Root, host: HTMLDivElement, cache: QueryClient, authority: DeliveryAuthority
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
        <ControllerWorkspace authority={authority} target={target} onClose={() => {}} />
      </QueryClientProvider>,
    ),
  )
  await flush()
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find((item) => item.textContent === label)
  if (!result) throw new Error(`Missing button ${label}`)
  return result
}
async function click(label: string) {
  await act(async () => button(label).click())
  await flush()
}
function control(label: string) {
  const labels = [...document.querySelectorAll('label')]
  const field = labels.find((item) => item.querySelector('span')?.textContent === label)
  if (!field) throw new Error(`Missing label ${label}`)
  return field.querySelector('input,textarea')! as HTMLInputElement | HTMLTextAreaElement
}
async function fill(label: string, value: string) {
  const element = control(label)
  await act(async () => {
    const prototype =
      element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await flush()
}
async function enrollForm() {
  await click('View controllers')
  await click('Enroll controller')
  await fill('Name', 'Reviewed machine')
  await fill('Reason', 'Reviewed exact configuration')
  const optIn = document.querySelector('[role="switch"]')!
  await act(async () => {
    ;(optIn as HTMLButtonElement).click()
  })
  await flush()
}
beforeEach(async () => {
  vi.resetAllMocks()
  await i18n.changeLanguage('en')
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], {
    user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'admin' },
    csrf_token: 'current-csrf',
  })
  cache.setQueryData(['permissions', 'usr_alice', 'controller'], ['secrets.read', 'secrets.write'])
  authority = {
    actor: 'usr_alice',
    admin: true,
    fresh: true,
    generation: 1,
    permissionKey: ['permissions', 'usr_alice', 'controller'],
  }
  api.getControllerSlots.mockResolvedValue({
    items: [
      {
        id: slot,
        birth,
        profile_id: target,
        owner_kind: 'personal',
        owner_id: id('usr'),
        descriptor_revision: '1',
        configuration_status: 'not_reviewed',
      },
    ],
    next_cursor: '',
  })
  api.getControllers.mockResolvedValue({
    items: [
      {
        id: controller,
        birth,
        name: view.name,
        slot_id: slot,
        expires_at: view.expires_at,
        revoked_at: null,
        lifecycle_status: 'active',
        configuration_status: 'not_reviewed',
      },
    ],
    next_cursor: '',
  })
  api.getControllerReview.mockResolvedValue({ configuration: config, review_etag: hash })
  api.getController.mockResolvedValue(view)
  api.enrollController.mockResolvedValue({
    controller: view,
    bearer: 'rxdctl_' + 'A'.repeat(43),
    secret_status: 'display_once',
  })
  api.assignController.mockResolvedValue(view)
  api.revokeController.mockResolvedValue({
    ...view,
    current_configuration: null,
    configuration_status: 'revoked',
    revoked_at: birth,
  })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await render()
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  cache.clear()
  await i18n.changeLanguage('en')
})
describe('controller administrator dialogs', () => {
  it('uses scoped tables and explicit review+confirmation before displaying one-time bearer locally', async () => {
    expect(api.getControllerSlots).toHaveBeenCalledWith(target, '', expect.any(AbortSignal))
    expect(api.getControllers).not.toHaveBeenCalled()
    await enrollForm()
    expect(api.enrollController).not.toHaveBeenCalled()
    await click('Review enrollment')
    expect(api.enrollController).not.toHaveBeenCalled()
    await click('Confirm exact operation')
    expect(api.enrollController).toHaveBeenCalledTimes(1)
    const [actualSlot, input, etag, csrf] = api.enrollController.mock.calls[0]
    expect(actualSlot).toBe(slot)
    expect(input).toMatchObject({
      name: 'Reviewed machine',
      reason: 'Reviewed exact configuration',
      opt_in: true,
      descriptor_revision: '1',
      configuration_digest: hash,
    })
    expect(input.request_id).toMatch(/^[a-f0-9-]{36}$/)
    expect(etag).toBe(hash)
    expect(csrf).toBe('current-csrf')
    const field = document.querySelector<HTMLInputElement>('input[type="password"]')
    expect(field?.value).toBe('rxdctl_' + 'A'.repeat(43))
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('rxdctl_')
    await click('I have saved it; clear local bearer')
    expect(document.querySelector('input[type="password"]')).toBeNull()
  })
  it('renders recorded advisory report separately and revokes only exact reviewed controller after confirmation', async () => {
    await click('View controllers')
    expect(document.body.textContent).toContain('Within credential lifetime')
    expect(document.body.textContent).toContain('Configuration not reviewed')
    await click('Review controller')
    expect(document.body.textContent).toContain('Reported written')
    expect(document.body.textContent).toContain('does not prove Agent output')
    await fill('Reason', 'Reviewed independent credential revocation')
    await click('Review credential revocation')
    expect(api.revokeController).not.toHaveBeenCalled()
    await click('Confirm exact operation')
    expect(api.revokeController).toHaveBeenCalledWith(
      controller,
      slot,
      'Reviewed independent credential revocation',
      hash,
      'current-csrf',
      expect.any(AbortSignal),
    )
  })
  it('keeps independent read-only navigation while write actions remain disabled', async () => {
    await act(async () => cache.setQueryData(authority.permissionKey, ['secrets.read']))
    await flush()
    await click('View controllers')
    expect(button('Enroll controller').disabled).toBe(true)
    await click('Review controller')
    await fill('Reason', 'Read-only reason')
    expect(button('Review credential revocation').disabled).toBe(true)
    expect(api.revokeController).not.toHaveBeenCalled()
    expect(api.enrollController).not.toHaveBeenCalled()
  })
  it('changes language live without dropping the name or reason draft', async () => {
    await enrollForm()
    await act(async () => i18n.changeLanguage('zh'))
    await flush()
    expect(control('名称').value).toBe('Reviewed machine')
    expect(control('原因').value).toBe('Reviewed exact configuration')
    expect(button('审阅注册').disabled).toBe(false)
    expect(document.body.textContent).not.toContain('keyControllers:')
  })
  it('keeps a rejected original enrollment available only for identical explicit retry and null-bearer lookup', async () => {
    api.enrollController
      .mockRejectedValueOnce(new Error('lost response'))
      .mockResolvedValueOnce({ controller: view, bearer: null, secret_status: 'not_recoverable' })
    await enrollForm()
    await click('Review enrollment')
    await click('Confirm exact operation')
    const original = structuredClone(api.enrollController.mock.calls[0][1])
    expect(document.body.textContent).toContain('Original controller outcome unknown')
    expect(document.querySelector('input[type="password"]')).toBeNull()
    await click('Retry original operation')
    expect(api.enrollController.mock.calls[1][1]).toEqual(original)
    expect(api.enrollController.mock.calls[1][2]).toBe(hash)
    expect(document.body.textContent).toContain('cannot be recovered')
    expect(document.querySelector('input[type="password"]')).toBeNull()
  })
})
