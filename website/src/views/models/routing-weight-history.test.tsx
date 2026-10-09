import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import RoutingWeightHistory from './routing-weight-history'
import AdminModelsPage from './admin'
import { MemoryRouter, Route, Routes } from 'react-router'
import {
  birth,
  detailFixture,
  modelID,
  resultFixture,
  reviewETag,
  reviewFixture,
  versionID,
} from './routing-weight-history-fixtures'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type {
  ModelWeightRollbackInput,
  ModelWeightReview,
  ModelWeightRollbackResult,
} from '@/types/model-weight-history'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
let root: Root, container: HTMLDivElement, cache: QueryClient, calls: InternalAxiosRequestConfig[]
let review: ModelWeightReview,
  result: ModelWeightRollbackResult | null,
  getFail: boolean,
  postFail: boolean,
  recoveryFail: boolean,
  wait: Promise<void> | null
let props: {
  actor: string
  modelID: string
  modelBirth: string | null | undefined
  generation: number
  permissionGeneration: number
  resourceGeneration: number
  readable: boolean
  canWrite: boolean
}
let authority: boolean
beforeEach(async () => {
  await i18n.changeLanguage('en')
  calls = []
  review = reviewFixture()
  result = null
  getFail = postFail = recoveryFail = false
  wait = null
  authority = true
  props = {
    actor: 'usr_history',
    modelID,
    modelBirth: birth,
    generation: 1,
    permissionGeneration: 1,
    resourceGeneration: 1,
    readable: true,
    canWrite: true,
  }
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, {
    user: { id: props.actor, role: 'admin' },
    csrf_token: 'first-csrf',
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client.defaults.adapter = async (config) => {
    calls.push(config)
    const path = config.url!,
      post = config.method === 'post',
      recovery = path.includes('/rollback-commands/')
    if ((post || recovery || path.includes('/rollback-review')) && wait) await wait
    const failed = post ? postFail : recovery ? recoveryFail : getFail
    const response = {
      config,
      status: failed ? 503 : 200,
      statusText: '',
      headers: new AxiosHeaders({
        'Cache-Control': 'private, no-store',
        ETag: '"' + review.review_etag + '"',
      }),
      data: {} as unknown,
    }
    if (failed) throw new AxiosError('Request failed', '', config, undefined, response)
    if (post) {
      result = resultFixture(JSON.parse(config.data) as ModelWeightRollbackInput)
      response.data = structuredClone(result)
    } else if (recovery) {
      if (!result)
        throw new AxiosError('Unknown original', '', config, undefined, {
          ...response,
          status: 404,
        })
      response.data = structuredClone(result)
    } else if (path.includes('/rollback-review')) response.data = structuredClone(review)
    else if (path.includes('/weight-versions?'))
      response.data = {
        model_id: props.modelID,
        items: [detailFixture().version],
        next_cursor: null,
      }
    else if (path.includes('/weight-versions/')) response.data = detailFixture()
    else throw new Error('Unexpected scoped request')
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = adapter
  await i18n.changeLanguage('en')
})
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RoutingWeightHistory
          key={props.actor + ':' + props.modelID}
          {...props}
          open={true}
          onOpenChange={() => {}}
          readReady={() => authority && props.readable}
          writeReady={() => authority && props.readable && props.canWrite}
          onSaved={() => {}}
        />
      </QueryClientProvider>,
    ),
  )
}
async function until(check: () => void) {
  let error: unknown
  for (let n = 0; n < 100; n++) {
    try {
      check()
      return
    } catch (e) {
      error = e
    }
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
  }
  throw error
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (x) => x.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(value: string) {
  const input = document.querySelector<HTMLInputElement>('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select() {
  await render()
  await until(() => expect(button('Details')).toBeTruthy())
  await click('Details')
  await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
}
async function ready() {
  await select()
  await click('Review current restore eligibility')
  await until(() => expect(button('Restore reviewed weights')).toBeTruthy())
  await fill('Restore reviewed complete set')
}
async function restore() {
  await ready()
  await click('Restore reviewed weights')
  await click('Confirm restore')
}
function posts() {
  return calls.filter((x) => x.method === 'post')
}
function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

async function renderAdminHistory(permissions = ['models.read_all']) {
  const historyAdapter = client.defaults.adapter as (
    config: InternalAxiosRequestConfig,
  ) => Promise<unknown>
  client.defaults.adapter = async (config) => {
    if (config.url?.startsWith('/auth/') || config.url === '/admin/models/' + modelID) {
      calls.push(config)
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
        data:
          config.url === '/auth/session'
            ? {
                user: {
                  id: props.actor,
                  role: 'admin',
                  name: 'Current',
                  email: 'current@example.com',
                },
                csrf_token: 'current-csrf',
              }
            : config.url === '/auth/permissions'
              ? { permissions }
              : {
                  id: modelID,
                  created_at: birth,
                  config_updated_at: null,
                  name: 'Current Model',
                  status: 'active',
                  names: [{ name: 'Current Model', is_current: true, expires_at: null }],
                  bindings: [],
                  granted_user_ids: [],
                },
      }
    }
    return (await historyAdapter(config)) as never
  }
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/admin/models/' + modelID]}>
          <Routes>
            <Route path="/admin/models/:modelId" element={<AdminModelsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
}

describe('Reviewed weight restore in the existing Model detail composition', () => {
  it('does not automatically select, preview or restore a history row and uses no provider directory', async () => {
    await render()
    await until(() => expect(button('Details')).toBeTruthy())
    expect(document.body.textContent).toContain('Observed baseline (observation time)')
    expect(calls).toHaveLength(1)
    expect(calls[0].url).toContain('/weight-versions?limit=20')
    expect(posts()).toHaveLength(0)
    await click('Details')
    await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
    expect(document.body.textContent).toContain('pmd_A')
    expect(calls.some((x) => x.url?.includes('providers'))).toBe(false)
  })
  it('wires the secondary action beside legacy Save using only fresh Model read authority', async () => {
    await renderAdminHistory()
    await until(() => expect(button('History / Restore')).toBeTruthy())
    expect(button('Save routing weights').disabled).toBe(true)
    expect(calls.some((c) => c.url?.includes('weight-versions'))).toBe(false)
    await click('History / Restore')
    await until(() => expect(button('Details')).toBeTruthy())
    expect(calls.some((c) => c.url?.includes('providers'))).toBe(false)
    expect(posts()).toHaveLength(0)
    const trigger = button('History / Restore')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(
        true,
      ),
    )
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(document.activeElement).toBe(trigger))
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('历史 / 恢复')).toBe(trigger)
    await act(async () => {
      trigger.blur()
      trigger.click()
    })
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(
        true,
      ),
    )
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(document.activeElement).toBe(trigger))
    expect(posts()).toHaveLength(0)
  })
  it('restores focus to the remounted history trigger after a saved and separately read receipt', async () => {
    await renderAdminHistory(['models.read_all', 'models.write'])
    await until(() => expect(button('History / Restore')).toBeTruthy())
    const originalTrigger = button('History / Restore')
    const adminAdapter = client.defaults.adapter as (
      config: InternalAxiosRequestConfig,
    ) => Promise<unknown>
    const gate = deferred()
    let holdDetail = false
    let completedReceipt: ModelWeightRollbackResult | undefined
    client.defaults.adapter = async (config) => {
      if (holdDetail && config.url === '/admin/models/' + modelID) await gate.promise
      const response = await adminAdapter(config)
      if (config.method === 'get' && config.url?.includes('/rollback-commands/'))
        completedReceipt = (response as { data: ModelWeightRollbackResult }).data
      return response as never
    }
    try {
      await click('History / Restore')
      await until(() => expect(button('Details')).toBeTruthy())
      await click('Details')
      await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
      await click('Review current restore eligibility')
      await until(() => expect(button('Restore reviewed weights')).toBeTruthy())
      await fill('Restore reviewed complete set')
      await click('Restore reviewed weights')
      holdDetail = true
      await click('Confirm restore')
      await until(() => expect(originalTrigger.isConnected).toBe(false))
      const originalIntent = JSON.parse(posts()[0].data) as ModelWeightRollbackInput
      holdDetail = false
      await act(async () => gate.resolve())
      await until(() => expect(button('Read original receipt')).toBeTruthy())
      await until(() => {
        expect(button('Read original receipt').disabled).toBe(false)
        expect(button('History / Restore')?.isConnected).toBe(true)
        const dialog = document.querySelector('[role="dialog"]')
        expect(dialog?.contains(button('Read original receipt'))).toBe(true)
        expect(
          dialog?.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled,
        ).toBe(false)
      })
      await click('Read original receipt')
      await until(() => {
        expect(completedReceipt?.receipt.request_id).toBe(originalIntent.request_id)
        expect(completedReceipt?.receipt.version_id).toBe(originalIntent.version_id)
        expect(completedReceipt?.receipt.reason).toBe(originalIntent.reason)
        expect(document.body.textContent).toContain(
          'The original result is applied to the current local serving configuration.',
        )
        expect(button('Read original receipt').disabled).toBe(false)
        expect(
          document.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]')
            ?.disabled,
        ).toBe(false)
      })
      await until(() =>
        expect(calls.some((call) => call.url?.includes('/rollback-commands/'))).toBe(true),
      )
      const receiptCalls = calls.filter((call) => call.url?.includes('/rollback-commands/'))
      expect(receiptCalls).toHaveLength(1)
      expect(receiptCalls[0].method).toBe('get')
      expect(receiptCalls[0].url).toBe(
        '/admin/models/' + modelID + '/weights/rollback-commands/' + originalIntent.request_id,
      )
      await until(() => expect(button('History / Restore')?.isConnected).toBe(true))
      const close = document.querySelector<HTMLButtonElement>(
        '[role="dialog"] button[aria-label="Close"]',
      )!
      await act(async () => close.focus())
      expect(document.activeElement).toBe(close)
      await until(() =>
        expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(
          true,
        ),
      )
      const currentTrigger = button('History / Restore')
      expect(currentTrigger).not.toBe(originalTrigger)
      expect(currentTrigger.disabled).toBe(false)
      expect(posts()).toHaveLength(1)
      expect(document.body.textContent).toContain(JSON.parse(posts()[0].data).request_id)
      await act(async () => {
        close.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      })
      await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
      await until(() => expect(document.activeElement).toBe(currentTrigger))
    } finally {
      await act(async () => gate.resolve())
    }
  })
  it('preserves the first UUID on rapid duplicate confirmation clicks', async () => {
    await ready()
    await click('Restore reviewed weights')
    const gate = deferred()
    wait = gate.promise
    const confirm = button('Confirm restore')
    await act(async () => {
      confirm.click()
      confirm.click()
    })
    expect(posts()).toHaveLength(1)
    const original = JSON.parse(posts()[0].data)
    expect(document.body.textContent).toContain(original.request_id)
    wait = null
    await act(async () => gate.resolve())
    await until(() => expect(document.body.textContent).toContain('Saved command: changed weights'))
    expect(document.body.textContent).toContain(original.request_id)
  })
  it('rejects authority lost immediately before a confirmation event without sending a command', async () => {
    await ready()
    await click('Restore reviewed weights')
    authority = false
    await click('Confirm restore')
    expect(posts()).toHaveLength(0)
  })
  it('discards a late preview after renewal and requires another explicit current review', async () => {
    await select()
    const gate = deferred()
    wait = gate.promise
    await click('Review current restore eligibility')
    props.readable = false
    authority = false
    await render()
    props.generation++
    props.permissionGeneration++
    props.resourceGeneration++
    props.readable = true
    authority = true
    await render()
    wait = null
    await act(async () => gate.resolve())
    await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
    expect(button('Restore reviewed weights')).toBeUndefined()
    expect(button('Confirm restore')).toBeUndefined()
    expect(posts()).toHaveLength(0)
  })
  it('uses fresh server comparison, required reason and a separate explicit confirmation before one command', async () => {
    await ready()
    expect(posts()).toHaveLength(0)
    await click('Restore reviewed weights')
    expect(posts()).toHaveLength(0)
    await click('Confirm restore')
    await until(() => expect(document.body.textContent).toContain('Saved command: changed weights'))
    expect(posts()).toHaveLength(1)
    expect(posts()[0].headers.get('If-Match')).toBe('"' + reviewETag + '"')
    const body = JSON.parse(posts()[0].data)
    expect(body).toMatchObject({ version_id: versionID, reason: 'Restore reviewed complete set' })
    expect(body.request_id).toMatch(/^[0-9a-f-]{36}$/)
    expect(document.body.textContent).toContain('not prove native completion')
    expect(document.body.textContent).toContain('current local serving configuration')
  })
  it('blocks unknown current blockers without inferring safety from structurally valid history', async () => {
    review.eligible = false
    review.blocker_codes = ['new_safety_guard']
    await ready()
    expect(button('Restore reviewed weights').disabled).toBe(true)
    expect(document.body.textContent).toContain('unrecognized server blocker')
    expect(posts()).toHaveLength(0)
  })
  it('shows all current and proposed rows separately when topology blocks restoration', async () => {
    review.eligible = false
    review.blocker_codes = ['topology_changed']
    review.current_weights.push({
      ...review.current_weights[1],
      binding_id: 'bnd_new',
      provider_model_id: 'pmd_new',
    })
    await ready()
    expect(
      document.querySelector('table[aria-label="Complete current weight set"]')?.textContent,
    ).toContain('bnd_new')
    expect(
      document.querySelector('table[aria-label="Complete proposed weight set"]')?.textContent,
    ).not.toContain('bnd_new')
    expect(button('Restore reviewed weights').disabled).toBe(true)
    expect(posts()).toHaveLength(0)
  })
  it('keeps history readable without independent write permission and allows read-only current review', async () => {
    props.canWrite = false
    review.eligible = false
    review.can_rollback = false
    review.blocker_codes = ['write_not_authorized']
    await select()
    await click('Review current restore eligibility')
    await until(() => expect(button('Restore reviewed weights')).toBeTruthy())
    expect(button('Restore reviewed weights').disabled).toBe(true)
    expect(posts()).toHaveLength(0)
  })
  it('cannot confirm a changed reason without a new explicit confirmation', async () => {
    await ready()
    await click('Restore reviewed weights')
    await fill('Different reason')
    expect(button('Confirm restore')).toBeUndefined()
    expect(posts()).toHaveLength(0)
  })
  it('hides private facts during renewal, preserves a conflict reason, and requires explicit fresh review', async () => {
    await ready()
    await click('Restore reviewed weights')
    props.readable = false
    authority = false
    await render()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    props.readable = true
    authority = true
    props.generation++
    props.permissionGeneration++
    props.resourceGeneration++
    await render()
    await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
    expect(button('Confirm restore')).toBeUndefined()
    expect(button('Restore reviewed weights')).toBeUndefined()
    expect(calls.filter((x) => x.url?.includes('rollback-review'))).toHaveLength(1)
    await click('Review current restore eligibility')
    await until(() =>
      expect(document.querySelector<HTMLInputElement>('input')?.value).toBe(
        'Restore reviewed complete set',
      ),
    )
    expect(posts()).toHaveLength(0)
  })
  it('hides cached selected facts and eligibility while renewed history/detail/review fails', async () => {
    await ready()
    getFail = true
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'model-weight-history'] })
    })
    await until(() =>
      expect(document.body.textContent).toContain(
        'Weight history or current review is unavailable',
      ),
    )
    expect(
      document.querySelector('section[aria-label="Current and proposed complete weights"]'),
    ).toBeNull()
    expect(button('Confirm restore')).toBeUndefined()
    expect(posts()).toHaveLength(0)
  })
  it('hides selected comparison during a history-only refresh and requires another fresh preview', async () => {
    await ready()
    await click('Restore reviewed weights')
    await act(async () => {
      await cache.invalidateQueries({
        predicate: (q) =>
          q.queryKey.at(-1) === undefined && q.queryKey[1] === 'model-weight-history',
      })
    })
    await until(() => expect(button('Review current restore eligibility')).toBeTruthy())
    expect(button('Confirm restore')).toBeUndefined()
    expect(button('Restore reviewed weights')).toBeUndefined()
    expect(
      document.querySelector('section[aria-label="Current and proposed complete weights"]'),
    ).toBeNull()
    await click('Review current restore eligibility')
    await until(() => expect(button('Restore reviewed weights')).toBeTruthy())
    expect(posts()).toHaveLength(0)
  })
  it('blocks a preview whose proposed rows contradict the selected retained version', async () => {
    review.proposed_weights[0].weight = 1
    review.proposed_weights[1].weight = 99
    await ready()
    expect(button('Restore reviewed weights').disabled).toBe(true)
    expect(posts()).toHaveLength(0)
  })
  it('keeps original UUID/body/IfMatch through uncertain POST, rejected retries and recovery 404', async () => {
    postFail = true
    await restore()
    await until(() =>
      expect(document.body.textContent).toContain('original command outcome is uncertain'),
    )
    const first = posts()[0]
    expect(button('Start another review')).toBeUndefined()
    await click('Read original receipt')
    await until(() => expect(document.body.textContent).toContain('command result is unavailable'))
    await click('Retry original command')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(button('Start another review')).toBeUndefined()
    postFail = false
    await click('Retry original command')
    await until(() => expect(document.body.textContent).toContain('Saved command: changed weights'))
    expect(posts()[2].data).toBe(first.data)
  })
  it('retains uncertain dispatched intent on same-owner renewal and retries only manually with fresh CSRF', async () => {
    const gate = deferred()
    await ready()
    wait = gate.promise
    await click('Restore reviewed weights')
    await click('Confirm restore')
    expect(posts()).toHaveLength(1)
    const first = posts()[0]
    props.readable = false
    authority = false
    await render()
    props.generation++
    props.permissionGeneration++
    props.resourceGeneration++
    props.readable = true
    authority = true
    cache.setQueryData(sessionKey, {
      user: { id: props.actor, role: 'admin' },
      csrf_token: 'renewed-csrf',
    })
    await render()
    wait = null
    await act(async () => gate.resolve())
    await until(() => expect(button('Retry original command')?.disabled).toBe(false))
    expect(document.body.textContent).toContain('original command outcome is uncertain')
    expect(document.body.textContent).not.toContain('Saved command: changed weights')
    expect(posts()).toHaveLength(1)
    await click('Retry original command')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  })
  it('allows original GET recovery after write revocation, keeps saved pending separate and never auto redispatches', async () => {
    await restore()
    await until(() => expect(button('Start another review')).toBeTruthy())
    result!.application_status = 'pending'
    result!.runtime_applied = false
    props.canWrite = false
    await render()
    await click('Read original receipt')
    await until(() => expect(document.body.textContent).toContain('local application is pending'))
    expect(button('Retry original command').disabled).toBe(true)
    expect(button('Start another review')).toBeUndefined()
    expect(posts()).toHaveLength(1)
    const get = calls.find((x) => x.url?.includes('/rollback-commands/'))!
    expect(get.headers.get('X-CSRF-Token')).toBeUndefined()
    expect(get.data).toBeUndefined()
  })
  it('retains a known durable receipt but marks local application unknown after failed recovery or retry', async () => {
    await restore()
    await until(() => expect(button('Start another review')).toBeTruthy())
    const original = JSON.parse(posts()[0].data)
    recoveryFail = true
    await click('Read original receipt')
    await until(() => expect(document.body.textContent).toContain('command result is unavailable'))
    expect(document.body.textContent).toContain('Saved command: changed weights')
    expect(document.body.textContent).toContain('Current local application is unknown')
    expect(button('Start another review')).toBeUndefined()
    expect(document.body.textContent).toContain(original.request_id)
    postFail = true
    await click('Retry original command')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(posts()[0].data)
    expect(document.body.textContent).not.toContain('result is applied to the current local')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('superseded receipt never automatically reapplies old weights or generates another UUID', async () => {
    await restore()
    await until(() => expect(button('Start another review')).toBeTruthy())
    result!.application_status = 'superseded'
    result!.runtime_applied = false
    await click('Read original receipt')
    await until(() => expect(document.body.textContent).toContain('newer configuration superseded'))
    expect(posts()).toHaveLength(1)
    expect(document.body.textContent).toContain('not restore the old set again')
  })
  it('discards late command callbacks and retained private intent after actor or Model birth changes', async () => {
    const gate = deferred()
    await ready()
    wait = gate.promise
    await click('Restore reviewed weights')
    await click('Confirm restore')
    props.actor = 'usr_other'
    props.readable = false
    authority = false
    await render()
    await act(async () => gate.resolve())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    props.actor = 'usr_history'
    props.readable = true
    authority = true
    wait = null
    await render()
    await until(() => expect(button('Details')).toBeTruthy())
    expect(document.body.textContent).not.toContain('Original restore command')
    await ready()
    postFail = true
    await click('Restore reviewed weights')
    await click('Confirm restore')
    await until(() => expect(button('Retry original command')).toBeTruthy())
    props.modelBirth = '2026-10-02T01:02:03Z'
    await render()
    expect(button('Retry original command')).toBeUndefined()
    expect(document.body.textContent).not.toContain('Original restore command')
  })
  it('event-time authority loss retains the original command and receipt before starting another review', async () => {
    await restore()
    await until(() => expect(button('Start another review')).toBeTruthy())
    const original = JSON.parse(posts()[0].data)
    const count = calls.length
    // Revoke after the enabled render, without refreshing the handler or DOM.
    authority = false
    await click('Start another review')
    expect(calls).toHaveLength(count)
    expect(posts()).toHaveLength(1)
    expect(document.body.textContent).toContain(original.request_id)
    expect(document.body.textContent).toContain(original.reason)
    expect(document.body.textContent).toContain('Saved command: changed weights')
    props.readable = false
    await render()
    props.generation++
    props.permissionGeneration++
    props.resourceGeneration++
    props.readable = true
    authority = true
    await render()
    expect(document.body.textContent).toContain(original.request_id)
    expect(document.body.textContent).toContain('Current local application is unknown')
    expect(button('Start another review')).toBeUndefined()
    await click('Read original receipt')
    await until(() => expect(button('Start another review')).toBeTruthy())
    const freshCount = calls.length
    await click('Start another review')
    await until(() => expect(button('Details')).toBeTruthy())
    expect(calls).toHaveLength(freshCount + 1)
    expect(posts()).toHaveLength(1)
    expect(result!.receipt.request_id).toBe(original.request_id)
    expect(result!.receipt.reason).toBe(original.reason)
  })
  it('event-time authority loss blocks imperative history refresh until current authority returns', async () => {
    await render()
    await until(() => expect(button('Refresh history')?.disabled).toBe(false))
    const count = calls.length
    authority = false
    await click('Refresh history')
    expect(calls).toHaveLength(count)
    expect(posts()).toHaveLength(0)
    authority = true
    await render()
    await click('Refresh history')
    await until(() => expect(calls).toHaveLength(count + 1))
    expect(calls.at(-1)!.url).toContain('/weight-versions?limit=20')
    expect(posts()).toHaveLength(0)
  })
  it('switches paired copy live without clearing selected version or reason', async () => {
    await ready()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector<HTMLInputElement>('input')?.value).toBe(
      'Restore reviewed complete set',
    )
    expect(document.body.textContent).toContain('当前与拟恢复的完整权重')
    expect(button('恢复已审核权重')).toBeTruthy()
    expect(posts()).toHaveLength(0)
  })
})
