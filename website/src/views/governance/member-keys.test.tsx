import { accessSummaryFixture } from './member-access-summary.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import {
  memberKeyFixture,
  memberKeysActor,
  memberKeysID,
  memberKeysUser,
} from './member-keys.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[],
  permissions: string[],
  actor: string,
  csrf: string,
  row: ReturnType<typeof memberKeyFixture>,
  postStatus: number,
  malformed: boolean,
  commitOnFailure: boolean,
  hasNext: boolean,
  listStatus: number,
  detailStatus: number,
  permissionStatus: number,
  sessionStatus: number
let sessionGate: ReturnType<typeof deferred> | undefined,
  listGate: ReturnType<typeof deferred> | undefined,
  postGate: ReturnType<typeof deferred> | undefined,
  gates: ReturnType<typeof deferred>[]
const original = client.defaults.adapter
function deferred(): { promise: Promise<void>; release: () => void } {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
function fail(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private hidden error', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'private hidden error' },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  gates = []
  actor = memberKeysActor
  csrf = 'csrf-first'
  permissions = ['members.read', 'members.keys.disable']
  row = memberKeyFixture()
  postStatus = 0
  malformed = commitOnFailure = hasNext = false
  listStatus = detailStatus = permissionStatus = sessionStatus = 0
  sessionGate = listGate = postGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (config.method === 'get' && config.url?.endsWith('/access')) {
      const data = accessSummaryFixture(config.url.split('/')[3], {
        roles: permissions.includes('roles.read'),
        teams: permissions.includes('teams.read_all'),
      })
      if (data.roles.status === 'available') data.roles.items = []
      if (data.teams.status === 'available') data.teams.items = []
      return {
        config: config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
        data,
      }
    }
    let data: unknown,
      revision = row.etag
    if (config.url === '/auth/session') {
      if (sessionStatus) throw fail(config, sessionStatus)
      data = { user: { id: actor, name: 'Admin', role: 'admin' }, csrf_token: csrf }
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      if (permissionStatus) throw fail(config, permissionStatus)
      data = { permissions: [...permissions] }
    } else if (config.url === `/admin/members/${memberKeysUser}`)
      data = {
        id: memberKeysUser,
        name: 'Target',
        email: 'target@example.invalid',
        role: 'member',
        disabled: false,
        offboarded_at: null,
        created_at: row.created_at,
        role_ids: [],
        registration_approval: { status: 'not_required', admission_eligible: false },
        last_login_at: null,
        last_login_status: 'historical_unavailable',
      }
    else if (config.url === `/admin/members/${memberKeysUser}/keys`) {
      if (listStatus) throw fail(config, listStatus)
      data = config.params?.cursor
        ? {
            items: [
              {
                ...structuredClone(row),
                id: 'key_01bbbbbbbbbbbbbbbbbbbbbbbb',
                name: 'Older Application',
              },
            ],
            next_cursor: null,
          }
        : { items: [structuredClone(row)], next_cursor: hasNext ? row.id : null }
      if (listGate) await listGate.promise
    } else if (config.url === `/admin/members/${memberKeysUser}/keys/${memberKeysID}`) {
      if (detailStatus) throw fail(config, detailStatus)
      data = structuredClone(row)
    } else if (config.url === `/admin/members/${memberKeysUser}/keys/${memberKeysID}/disable`) {
      if (postGate) await postGate.promise
      if (postStatus) {
        if (commitOnFailure)
          row = { ...row, status: 'disabled', disable_eligible: false, etag: 'd'.repeat(64) }
        throw fail(config, postStatus)
      }
      revision = 'b'.repeat(64)
      data = {
        user_id: memberKeysUser,
        id: memberKeysID,
        status: 'disabled',
        etag: revision,
        runtime_applied: !malformed,
        confirmation: 'current_disabled_state',
      }
      row = { ...row, status: 'disabled', disable_eligible: false, etag: revision }
    } else if (config.url?.startsWith('/admin/members/')) throw fail(config, 404)
    else throw new Error(`Unexpected endpoint ${config.url}`)
    return {
      config,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ ETag: `"${revision}"` }),
    }
  }
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: [`/admin/members/${memberKeysUser}?tab=keys`],
  })
})
afterEach(async () => {
  for (const g of gates) g.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function settle() {
  for (let i = 0; i < 6; i++)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
const button = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>('button, [role="menuitem"]')).find(
    (b) => b.textContent?.trim() === label,
  )!
async function click(el: Element) {
  expect(el).toBeTruthy()
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await settle()
}
async function reason(value: string) {
  const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function open() {
  await click(document.querySelector('[aria-label="Actions for Application"]')!)
  await click(button('Disable API Key'))
  await settle()
}
const posts = () => requests.filter((r) => r.method === 'post')
it('renders exact scoped table, ceilings, zero/unlimited, monthly and separate live holds without directories', async () => {
  await render()
  const text = host.textContent!
  expect(text).toContain('9007199254740993 / 0 per month')
  expect(text).toContain('5.000000000000000002 USD')
  expect(text).toContain('0.000000000000000001 USD')
  expect(text).toContain('Live token holds: 5')
  expect(text).toContain('Coverage incomplete')
  expect(text).toContain('Shared rotation quota')
  expect(text).toContain('mdl_recorded')
  expect(
    requests.some((r) => r.url === '/models' || r.url === '/keys' || r.url === '/admin/models'),
  ).toBe(false)
})
it('read permission never implies disable and disable never implies read', async () => {
  permissions = ['members.read']
  await render()
  expect(host.textContent).toContain('Application')
  expect(document.querySelector('[aria-label="Actions for Application"]')).toBeNull()
  permissions = ['members.keys.disable']
  await act(async () => cache.invalidateQueries({ queryKey: ['member-keys-permissions'] }))
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(posts()).toHaveLength(0)
})
it.each(['pending', 'disabled', 'revoked', 'expired'])(
  'cannot start a new disable for %s',
  async (status) => {
    if (status === 'expired') row.expired = true
    else row.status = status as typeof row.status
    row.disable_eligible = false
    await render()
    await click(document.querySelector('[aria-label="Actions for Application"]')!)
    expect(
      button('Disable unavailable').getAttribute('aria-disabled') ??
        button('Disable unavailable').hasAttribute('disabled').toString(),
    ).toBeTruthy()
    expect(posts()).toHaveLength(0)
  },
)
it('fresh reviewed revision and reason are submitted once, confirming current state only', async () => {
  await render()
  await open()
  await reason(' Stop application ')
  await click(button('Confirm disable'))
  expect(posts()).toHaveLength(1)
  expect(posts()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  expect(JSON.parse(posts()[0].data)).toEqual({ reason: 'Stop application' })
  expect(host.textContent).toContain('currently disabled')
  expect(host.textContent).toContain('original historical operation')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it('language switch keeps the reason and displays current translated dialog copy', async () => {
  await render()
  await open()
  await reason('保留原因')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.value).toBe('保留原因')
  expect(document.body.textContent).toContain('确认禁用')
  expect(document.body.textContent).not.toContain('Confirm disable')
})
it('503 and rejected retries retain exact immutable reason/revision and never offer review reset', async () => {
  await render()
  await open()
  await reason('Original reason')
  postStatus = 503
  await click(button('Confirm disable'))
  expect(button('Review latest')).toBeUndefined()
  expect(document.body.textContent).toContain('original request is unconfirmed')
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()).toHaveLength(2)
  expect(posts()[1].data).toBe(posts()[0].data)
  expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.disabled).toBe(true)
  expect(host.textContent).not.toContain('currently disabled')
})
it('fresh conflict retains draft and needs explicit new review', async () => {
  await render()
  await open()
  await reason('Reason retained')
  postStatus = 409
  await click(button('Confirm disable'))
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.value).toBe(
    'Reason retained',
  )
  expect(button('Confirm disable').disabled).toBe(true)
  row.etag = 'c'.repeat(64)
  await click(button('Review latest'))
  postStatus = 0
  await click(button('Confirm disable'))
  expect(posts()[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
})
it('malformed success is uncertain and not optimistically confirmed', async () => {
  await render()
  await open()
  await reason('Reason')
  malformed = true
  await click(button('Confirm disable'))
  expect(document.body.textContent).toContain('original request is unconfirmed')
  expect(host.textContent).not.toContain('currently disabled')
})
it('same-ms genuine Session renewal hides private facts and restores original uncertainty with new CSRF', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1800000000000)
  await render()
  await open()
  await reason('Immutable draft')
  postStatus = 503
  await click(button('Confirm disable'))
  const initial = posts()[0]
  csrf = 'csrf-new'
  sessionGate = deferred()
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  csrf = 'csrf-new'
  sessionGate.release()
  sessionGate = undefined
  await settle()
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()[1].data).toBe(initial.data)
  expect(posts()[1].headers.get('If-Match')).toBe(initial.headers.get('If-Match'))
  expect(posts()[1].headers.get('X-CSRF-Token')).toBe('csrf-new')
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
})
it('permission refresh hides a pending dialog, aborts its exact request and discards late success', async () => {
  await render()
  await open()
  await reason('Captured')
  postGate = deferred()
  const submit = button('Confirm disable')
  await act(async () => submit.click())
  await settle()
  const pending = posts()[0]
  permissions = ['members.read']
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys-permissions'] }))
  await settle()
  expect(pending.signal!.aborted).toBe(true)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  postGate.release()
  await settle()
  expect(host.textContent).not.toContain('currently disabled')
})
it('actor changes cannot restore a late private list or retain old intent', async () => {
  listGate = deferred()
  await render()
  actor = 'usr_01dddddddddddddddddddddddd'
  permissions = []
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
  await settle()
  listGate.release()
  listGate = undefined
  await settle()
  expect(posts()).toHaveLength(0)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(host.textContent).not.toContain('Application')
})

it('unavailable journal and absent policies are distinct from known zero', async () => {
  row.limits.quota_usage = null
  row.limits.effective.tokens_month = null
  row.limits.effective.money_month = null
  row.limits.effective.currency = ''
  row.last_use_coverage = 'no_recorded_use'
  await render()
  expect(host.textContent).toContain('Unknown / Unlimited per month')
  expect(host.textContent).toContain('Live token holds: Unknown')
  expect(host.textContent).toContain('No recorded use')
  expect(host.textContent).not.toContain('9007199254740993 / 0 per month')
})
it('shows stored and inherited effective policies without adding allowances', async () => {
  row.limits.stored.tokens_month = null
  row.limits.stored.money_month = null
  row.limits.stored.currency = ''
  row.limits.stored.rpm = null
  await render()
  expect(host.textContent).toContain('Stored monthly cap: Unlimited')
  expect(host.textContent).toContain('Stored monthly money cap: Unlimited')
  expect(host.textContent).toContain('9007199254740993 / 0 per month')
  expect(host.textContent).not.toContain('remaining')
})
it('invalid reason and duplicate confirmation never dispatch extra requests', async () => {
  await render()
  await open()
  await click(button('Confirm disable'))
  expect(posts()).toHaveLength(0)
  await reason('Ready')
  postGate = deferred()
  const confirm = button('Confirm disable')
  await act(async () => {
    confirm.click()
    confirm.click()
  })
  await settle()
  expect(posts()).toHaveLength(1)
  postGate.release()
  await settle()
})
it('permission errors hide prior rows and preserve uncertainty until fresh authority', async () => {
  await render()
  await open()
  await reason('Keep exact')
  postStatus = 503
  await click(button('Confirm disable'))
  const first = posts()[0]
  permissionStatus = 503
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys-permissions'] }))
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  permissionStatus = 0
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys-permissions'] }))
  await settle()
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()[1].data).toBe(first.data)
  expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
})
it('list failure hides private rows and does not render an empty successful list', async () => {
  await render()
  listStatus = 503
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys'] }))
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(host.textContent).not.toContain('No Personal Keys')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it('target switch aborts pending POST and ignores its late success', async () => {
  await render()
  await open()
  await reason('Exact original target')
  postGate = deferred()
  await act(async () => button('Confirm disable').click())
  await settle()
  const old = posts()[0]
  await act(async () => router.navigate('/admin/members/usr_01dddddddddddddddddddddddd?tab=keys'))
  await settle()
  expect(old.signal!.aborted).toBe(true)
  postGate.release()
  await settle()
  expect(host.textContent).not.toContain('currently disabled')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it('revoked Session destroys private state and cannot accept late POST success', async () => {
  await render()
  await open()
  await reason('Session bound')
  postGate = deferred()
  await act(async () => button('Confirm disable').click())
  await settle()
  const old = posts()[0]
  sessionStatus = 401
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
  await settle()
  expect(old.signal!.aborted).toBe(true)
  postGate.release()
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(host.textContent).not.toContain('currently disabled')
})
it('Escape dismisses the local dialog without dispatch or confirmation', async () => {
  await render()
  await open()
  await act(async () => {
    document
      .querySelector('[role="dialog"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  await settle()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(posts()).toHaveLength(0)
})

it.each(['session', 'permissions', 'target'])(
  'same-batch %s refresh prevents stale confirmation before paint',
  async (kind) => {
    await render()
    await open()
    await reason('Fresh authority')
    const confirm = button('Confirm disable')
    await act(async () => {
      void cache.refetchQueries({
        queryKey:
          kind === 'session'
            ? ['auth', 'session']
            : kind === 'permissions'
              ? ['member-keys-permissions']
              : ['admin', 'member', memberKeysActor, memberKeysUser],
      })
      confirm.click()
    })
    await settle()
    expect(posts()).toHaveLength(0)
  },
)
it('incidental Session outage retains immutable uncertain intent through later fresh reads', async () => {
  await render()
  await open()
  await reason('Immutable reason')
  postStatus = 503
  await click(button('Confirm disable'))
  const original = posts()[0]
  sessionStatus = 503
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
  await settle()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(host.textContent).not.toContain('Application')
  sessionStatus = 0
  csrf = 'renewed'
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
  await settle()
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()[1].data).toBe(original.data)
  expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(posts()[1].headers.get('X-CSRF-Token')).toBe('renewed')
})
it.each(['outage', 'denied'])(
  'parent authority %s hides private state without discarding the original uncertain request',
  async (mode) => {
    await render()
    await open()
    await reason('Original parent-bound reason')
    postStatus = 503
    await click(button('Confirm disable'))
    const original = posts()[0]
    if (mode === 'outage') permissionStatus = 503
    else permissions = []
    await act(async () => cache.refetchQueries({ queryKey: ['permissions', memberKeysActor] }))
    await settle()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('Application')
    expect(button('Retry original request')).toBeUndefined()
    expect(posts()).toHaveLength(1)
    permissionStatus = 0
    permissions = ['members.read', 'members.keys.disable']
    await act(async () => cache.refetchQueries({ queryKey: ['permissions', memberKeysActor] }))
    await settle()
    postStatus = 409
    await click(button('Retry original request'))
    expect(posts()).toHaveLength(2)
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe(original.headers.get('X-CSRF-Token'))
  },
)
it('a committed-but-unconfirmed disabled row can only reconcile the captured original request', async () => {
  await render()
  await open()
  await reason('Original committed reduction')
  postStatus = 503
  commitOnFailure = true
  await click(button('Confirm disable'))
  const original = posts()[0]
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys'] }))
  await act(async () => cache.refetchQueries({ queryKey: ['member-key-review'] }))
  await settle()
  expect(button('Confirm disable')).toBeUndefined()
  expect(button('Review latest')).toBeUndefined()
  postStatus = 0
  await click(button('Retry original request'))
  expect(posts()[1].data).toBe(original.data)
  expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(host.textContent).toContain('currently disabled')
})
it('owner re-enable cannot replace the original uncertain revision with a newer review', async () => {
  await render()
  await open()
  await reason('Original reason')
  postStatus = 503
  await click(button('Confirm disable'))
  const original = posts()[0]
  row.etag = 'd'.repeat(64)
  await act(async () => cache.refetchQueries({ queryKey: ['member-key-review'] }))
  await settle()
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(button('Review latest')).toBeUndefined()
  expect(document.body.textContent).toContain('original request is unconfirmed')
})

it('bounded load-more sends the retained canonical cursor and keeps both scoped rows', async () => {
  hasNext = true
  await render()
  await click(button('Load more Keys'))
  expect(host.textContent).toContain('Older Application')
  const lists = requests.filter((r) => r.url === `/admin/members/${memberKeysUser}/keys`)
  expect(lists).toHaveLength(2)
  expect(lists[1].params.cursor).toBe(memberKeysID)
  expect(lists[1].params.limit).toBe(40)
  expect(button('Load more Keys')).toBeUndefined()
})
it('renewed authority does not reuse an earlier current-state success notice', async () => {
  await render()
  await open()
  await reason('Reviewed')
  await click(button('Confirm disable'))
  expect(host.textContent).toContain('currently disabled')
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
  await settle()
  expect(host.textContent).not.toContain('currently disabled')
  expect(posts()).toHaveLength(1)
})

it('review GET404 cannot confirm an uncertain disable or expose prior private rows', async () => {
  await render()
  await open()
  await reason('Unconfirmed original')
  postStatus = 503
  await click(button('Confirm disable'))
  const original = posts()[0]
  detailStatus = 404
  await act(async () => cache.refetchQueries({ queryKey: ['member-key-review'] }))
  await settle()
  expect(host.textContent).not.toContain('Application')
  expect(host.textContent).not.toContain('currently disabled')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(posts()).toHaveLength(1)
  detailStatus = 0
  await act(async () => cache.refetchQueries({ queryKey: ['member-key-review'] }))
  await settle()
  postStatus = 409
  await click(button('Retry original request'))
  expect(posts()[1].data).toBe(original.data)
  expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
})

it('a changed fresh list forces exact row review while retaining its unsent reason', async () => {
  await render()
  await open()
  await reason('Keep unsent reason')
  row.etag = 'd'.repeat(64)
  await act(async () => cache.refetchQueries({ queryKey: ['member-keys'] }))
  await settle()
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.value).toBe(
    'Keep unsent reason',
  )
  expect(button('Confirm disable').disabled).toBe(true)
  expect(posts()).toHaveLength(0)
  await click(button('Review latest'))
  await click(button('Confirm disable'))
  expect(posts()[0].headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
})

async function focusUntil(assertion: () => void) {
  await vi.waitFor(
    async () => {
      await act(async () => {})
      assertion()
    },
    { timeout: 1500, interval: 10 },
  )
}
it.each(['Escape', 'Cancel'])(
  'returns %s dismissal focus to the exact row trigger after the portal menu closes',
  async (action) => {
    hasNext = true
    await render()
    await click(button('Load more Keys'))
    const trigger = document.querySelector<HTMLButtonElement>(
      '[aria-label="Actions for Application"]',
    )!
    await act(async () => trigger.focus())
    await open()
    const currentTrigger = document.querySelector<HTMLButtonElement>(
      '[aria-label="Actions for Application"]',
    )!
    const sibling = document.querySelector<HTMLButtonElement>(
      '[aria-label="Actions for Older Application"]',
    )!
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(document.querySelector('[role="dialog"]')!.contains(document.activeElement)).toBe(true)
    if (action === 'Escape')
      await act(async () => {
        document.activeElement!.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
        )
      })
    else await click(button('Cancel'))
    await focusUntil(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await focusUntil(() => expect(document.activeElement).toBe(currentTrigger))
    expect(document.activeElement).not.toBe(sibling)
    expect(posts()).toHaveLength(0)
  },
)

it.each(['actor', 'target', 'permission'])(
  'does not return dialog focus to a disappeared %s scope',
  async (scope) => {
    await render()
    await open()
    const originalTrigger = document.querySelector<HTMLElement>(
      '[aria-label="Actions for Application"]',
    )!
    const focus = vi.spyOn(originalTrigger, 'focus')
    if (scope === 'actor') {
      actor = 'usr_other_actor'
      await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
    } else if (scope === 'target')
      await act(async () => router.navigate('/admin/members/usr_other_target?tab=keys'))
    else {
      permissions = ['members.read']
      await act(async () => cache.refetchQueries({ queryKey: ['member-keys-permissions'] }))
    }
    await focusUntil(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(originalTrigger.isConnected).toBe(false)
    expect(focus).not.toHaveBeenCalled()
    expect(posts()).toHaveLength(0)
  },
)
it('confirmed disable skips the disconnected row while the fresh authorized list is hidden', async () => {
  await render()
  await open()
  const originalTrigger = document.querySelector<HTMLElement>(
    '[aria-label="Actions for Application"]',
  )!
  const focus = vi.spyOn(originalTrigger, 'focus')
  await reason('Controlled focus confirmation')
  listGate = deferred()
  await click(button('Confirm disable'))
  await focusUntil(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(originalTrigger.isConnected).toBe(false)
  expect(document.querySelector('[aria-label="Actions for Application"]')).toBeNull()
  expect(focus).not.toHaveBeenCalled()
  expect(row.status).toBe('disabled')
  expect(posts()).toHaveLength(1)
  await act(async () => listGate!.release())
  await focusUntil(() =>
    expect(document.querySelector('[aria-label="Actions for Application"]')).not.toBeNull(),
  )
  expect(document.activeElement).not.toBe(originalTrigger)
  expect(focus).not.toHaveBeenCalled()
  expect(posts()).toHaveLength(1)
})
