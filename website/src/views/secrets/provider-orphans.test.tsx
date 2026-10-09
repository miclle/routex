import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { ProviderOrphans } from './provider-orphans'
const integration = 'vlt_01j00000000000000000000000',
  revision = 'vlr_01j00000000000000000000000',
  creation = '11111111-1111-4111-8111-111111111111',
  etag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
const observation = (succeeded = false) => ({
  attempted: succeeded,
  succeeded,
  duration_ms: '1',
  failure: null,
})
const orphan = () => ({
  creation_request_id: creation,
  kind: 'credential',
  provider_id: 'prv_01j00000000000000000000000',
  connection_id: 'con_01j00000000000000000000000',
  credential_id: 'crd_01j00000000000000000000000',
  integration_id: integration,
  revision_id: revision,
  created_at: '2026-10-07T00:00:00Z',
  state: 'orphan',
  write: observation(true),
  read: observation(true),
  ownership_recorded: true,
  eligible: true,
  blocker_codes: [] as string[],
  can_cleanup: true,
  review_etag: etag,
})
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  old: typeof client.defaults.adapter,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  value: ReturnType<typeof orphan>,
  actor: string,
  targetIntegration: string,
  admin: boolean,
  detailHeader: string | undefined,
  fresh: boolean,
  generation: number,
  status: number,
  detailStatus: number
let postGate: Promise<void> | undefined,
  releasePost: (() => void) | undefined,
  detailGate: Promise<void> | undefined,
  releaseDetail: (() => void) | undefined
let commandState = 'acknowledged',
  commandGETStatus = 200
let commands: Map<string, ReturnType<typeof receipt>>
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const settle = () =>
  act(async () => {
    await new Promise((done) => setTimeout(done, 25))
  })
const buttons = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].filter((b) => b.textContent === text)
async function click(text: string, index = 0) {
  expect(buttons(text)[index], text).toBeDefined()
  await act(async () => buttons(text)[index].click())
  await settle()
}
async function input(label: string, text: string) {
  const element = [...document.querySelectorAll<HTMLInputElement>('input')].find((e) =>
    e.parentElement?.textContent?.includes(label),
  )!
  expect(element, label).toBeDefined()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, text)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ProviderOrphans
          key={`${actor}:${targetIntegration}`}
          actor={actor}
          integration={targetIntegration}
          fresh={fresh}
          admin={admin}
          generation={generation}
          close={() => {
            root.render(
              <QueryClientProvider client={cache}>
                <span>closed</span>
              </QueryClientProvider>,
            )
          }}
        />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
async function review() {
  await click('Review current status')
  await input('Reason', 'Exact orphan cleanup')
  await click('Review cleanup')
  await input('Independent cleanup Token', 'cleanup-secret')
}
const posts = () => requests.filter((r) => r.method === 'post')
function receipt(requestID: string) {
  return {
    creation_request_id: creation,
    request_id: requestID,
    integration_id: integration,
    revision_id: revision,
    state: commandState,
    ownership: observation(commandState === 'acknowledged'),
    cleanup: {
      state: commandState === 'acknowledged' ? 'acknowledged' : 'unknown',
      observation: observation(commandState === 'acknowledged'),
    },
    started_at: '2026-10-07T01:00:00Z',
    finished_at:
      commandState === 'acknowledged' || commandState === 'failed' ? '2026-10-07T01:00:01Z' : null,
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_admin'
  targetIntegration = integration
  admin = true
  detailHeader = undefined
  fresh = true
  generation = 0
  permissions = ['secrets.read', 'secrets.write', 'providers.write']
  value = orphan()
  requests = []
  status = 200
  detailStatus = 200
  commands = new Map()
  commandState = 'acknowledged'
  commandGETStatus = 200
  postGate = undefined
  releasePost = undefined
  detailGate = undefined
  releaseDetail = undefined
  cache.setQueryData(sessionKey, {
    user: { id: actor, role: 'admin', name: 'Admin', email: 'admin@example.invalid' },
    csrf_token: 'c'.repeat(64),
  })
  old = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = (data: unknown, code = 200) => ({
      data,
      status: code,
      statusText: String(code),
      headers: { etag: detailHeader ?? `"${value.review_etag}"` },
      config,
    })
    if (config.url === '/auth/permissions') return response({ permissions: [...permissions] })
    if (config.method === 'post') {
      if (postGate) await postGate
      if (status !== 200 && status !== 202)
        throw new AxiosError('hidden', undefined, config, undefined, response({}, status))
      const requestID = JSON.parse(config.data).request_id
      const recorded = commands.get(requestID) ?? receipt(requestID)
      commands.set(requestID, recorded)
      return response(
        {
          receipt: recorded,
          running: recorded.state === 'pending',
        },
        status,
      )
    }
    if (config.url?.includes('/commands/')) {
      if (commandGETStatus !== 200)
        throw new AxiosError('hidden', undefined, config, undefined, response({}, commandGETStatus))
      const requestID = config.url.split('/').at(-1)!
      return response(commands.get(requestID) ?? receipt(requestID))
    }
    if (config.url?.includes('?limit=20'))
      return response({ items: [structuredClone(value)], next_cursor: null })
    const captured = structuredClone(value)
    if (detailGate) await detailGate
    if (detailStatus !== 200)
      throw new AxiosError('hidden', undefined, config, undefined, response({}, detailStatus))
    return response(captured)
  }
})
afterEach(async () => {
  releasePost?.()
  releaseDetail?.()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = old
  await i18n.changeLanguage('en')
})
it('previews with read-only authority and fetches no unrelated root inventory', async () => {
  permissions = ['secrets.read']
  value.can_cleanup = false
  await render()
  await click('Review current status')
  expect(document.body.textContent).toContain('Original ownership recorded')
  expect(buttons('Review cleanup')[0].disabled).toBe(true)
  expect(
    requests.every(
      (r) =>
        r.url === '/auth/permissions' ||
        r.url?.startsWith(`/admin/secrets/integrations/${integration}/provider-orphans`),
    ),
  ).toBe(true)
})
it.each([
  [['secrets.write', 'providers.write']],
  [['secrets.read', 'secrets.write']],
  [['secrets.read', 'providers.write']],
])('requires independent read and both write authorities %s', async (allowed) => {
  permissions = allowed
  await render()
  if (allowed.includes('secrets.read')) {
    await click('Review current status')
    await input('Reason', 'Exact orphan cleanup')
    expect(buttons('Review cleanup')[0].disabled).toBe(true)
  } else expect(document.body.textContent).not.toContain(creation)
  expect(posts()).toHaveLength(0)
})
it('fresh detail GET precedes danger confirmation and first Token is absent from caches/receipt/retry', async () => {
  await render()
  await review()
  expect(
    requests.filter((r) => r.method === 'get' && r.url?.endsWith('/' + creation)),
  ).toHaveLength(2)
  await click('Confirm cleanup')
  expect(posts()).toHaveLength(1)
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(JSON.parse(posts()[0].data).cleanup_token).toBe('cleanup-secret')
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state),
    ),
  ).not.toContain('cleanup-secret')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(document.body.textContent).toContain('Exact owned-version destroy acknowledged')
  await click('Reconcile exact command')
  expect(posts()).toHaveLength(2)
  expect(JSON.parse(posts()[1].data)).toEqual({
    request_id: JSON.parse(posts()[0].data).request_id,
    reason: 'Exact orphan cleanup',
  })
  expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
  expect(document.body.textContent).toContain(creation)
})
it('live EN/ZH switch preserves reason and confirmation Token locally', async () => {
  await render()
  await review()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('确认清理孤立对象')
  expect(document.querySelector<HTMLInputElement>('input[type=password]')?.value).toBe(
    'cleanup-secret',
  )
  expect(document.body.textContent).toContain('Exact orphan cleanup')
  await act(async () => i18n.changeLanguage('en'))
  await click('Close')
  expect(document.querySelector('input[type=password]')).toBeNull()
  await click('Review cleanup')
  expect(document.querySelector<HTMLInputElement>('input[type=password]')?.value).toBe('')
})
it('unknown blocker is localized generic and blocks confirmation', async () => {
  value.eligible = false
  value.blocker_codes = ['future_private_condition']
  await render()
  await click('Review current status')
  expect(document.body.textContent).toContain('unrecognized server condition')
  expect(document.body.textContent).not.toContain('future_private_condition')
  expect(buttons('Review cleanup')[0].disabled).toBe(true)
})
it('published eligible review permits only an explicit bounded drain attempt and clears its Token', async () => {
  value.state = 'committed'
  await render()
  await review()
  expect(document.body.textContent).toContain('This object was previously published')
  expect(document.body.textContent).toContain('not proof that in-flight uses have joined')
  expect(document.body.textContent).toContain('Permanently deny further use')
  expect(document.body.textContent).toContain('no destroy is requested')
  expect(document.body.textContent).not.toContain('This creation was never committed')
  expect(posts()).toHaveLength(0)
  await click('Confirm cleanup')
  expect(posts()).toHaveLength(1)
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state),
    ),
  ).not.toContain('cleanup-secret')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(document.body.textContent).toContain('version-1 destroy response')
  expect(document.body.textContent).toContain('does not prove path deletion')
})
it('published confirmation and drain guidance switch EN/ZH without replacing transient intent', async () => {
  value.state = 'committed'
  await render()
  await review()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('此对象曾发布')
  expect(document.body.textContent).toContain('若排空失败、被取消或仍未知，不会请求销毁')
  expect(document.querySelector<HTMLInputElement>('input[type=password]')?.value).toBe(
    'cleanup-secret',
  )
  expect(document.body.textContent).toContain('Exact orphan cleanup')
  expect(posts()).toHaveLength(0)
})
it.each([
  ['published_process_unproven', 'old or unknown process generation'],
  ['published_source_unavailable', 'original physical source'],
  ['published_drain_unproven', 'No destroy is permitted'],
])('published blocker %s is localized and never enables confirmation', async (code, guidance) => {
  value.state = 'committed'
  value.eligible = false
  value.blocker_codes = [code]
  await render()
  await click('Review current status')
  expect(document.body.textContent).toContain(guidance)
  expect(document.body.textContent).not.toContain(code)
  await input('Reason', 'Protected published object')
  expect(buttons('Review cleanup')[0].disabled).toBe(true)
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).not.toContain(code)
  expect(document.body.textContent).not.toContain(guidance)
  expect(posts()).toHaveLength(0)
})
it.each(['unknown', 'failed'])(
  'published %s receipt remains original after fresh review and receipt reads never redispatch',
  async (state) => {
    value.state = 'committed'
    commandState = state
    await render()
    await review()
    await click('Confirm cleanup')
    const original = JSON.parse(posts()[0].data)
    value.eligible = false
    value.blocker_codes = ['cleanup_claimed', 'published_drain_unproven']
    await click('Review current status', 1)
    expect(document.querySelector<HTMLInputElement>('section input')?.value).toBe(
      'Exact orphan cleanup',
    )
    expect(buttons('Review cleanup')).toHaveLength(0)
    await click('Review current status')
    expect(document.body.textContent).toContain(original.request_id)
    expect(buttons('Review cleanup')).toHaveLength(0)
    await click('Review recorded command')
    expect(posts()).toHaveLength(1)
    expect(
      requests.some((r) => r.method === 'get' && r.url?.endsWith('/' + original.request_id)),
    ).toBe(true)
    expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
    await click('Reconcile exact command')
    expect(posts()).toHaveLength(2)
    expect(JSON.parse(posts()[1].data)).toEqual({
      request_id: original.request_id,
      reason: original.reason,
    })
    expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
    expect(document.querySelector('input[type=password]')).toBeNull()
    expect(buttons('Review cleanup')).toHaveLength(0)
  },
)
it('new published drain blocker invalidates an already queued eligible confirmation', async () => {
  value.state = 'committed'
  await render()
  await review()
  const queued = buttons('Confirm cleanup')[0]
  value.eligible = false
  value.blocker_codes = ['published_drain_unproven']
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['admin', 'provider-orphan', actor, integration] }),
  )
  await settle()
  await act(async () => queued.click())
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(buttons('Review cleanup')[0].disabled).toBe(true)
  expect(posts()).toHaveLength(0)
})
it('failed or unknown receipt never removes row or reports success', async () => {
  for (const state of ['unknown', 'failed']) {
    commandState = state
    await render()
    await review()
    await click('Confirm cleanup')
    expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
    expect(document.body.textContent).toContain(creation)
    if (state === 'unknown') {
      await act(async () => root.render(<span />))
      await settle()
      cache.clear()
      cache.setQueryData(sessionKey, {
        user: { id: actor, role: 'admin' },
        csrf_token: 'c'.repeat(64),
      })
    }
  }
})
it('pending202 is retained, commandGET404 stays uncertain and retry remains token-free exact', async () => {
  commandState = 'pending'
  status = 202
  await render()
  await review()
  await click('Confirm cleanup')
  expect(document.body.textContent).toContain('still pending')
  commandGETStatus = 404
  await click('Review recorded command')
  expect(document.body.textContent).toContain('unconfirmed')
  await click('Reconcile exact command')
  expect(JSON.parse(posts()[1].data)).not.toHaveProperty('cleanup_token')
  expect(JSON.parse(posts()[1].data).request_id).toBe(JSON.parse(posts()[0].data).request_id)
})
it.each([
  [200, 'orphan'],
  [503, 'orphan'],
  [200, 'committed'],
  [503, 'committed'],
] as const)(
  'held dispatched result %s for %s during Session renewal preserves uncertain intent and releases busy',
  async (result, state) => {
    value.state = state
    await render()
    await review()
    postGate = new Promise((done) => {
      releasePost = done
    })
    status = result
    await act(async () => buttons('Confirm cleanup')[0].click())
    await settle()
    const original = posts()[0]
    fresh = false
    generation++
    await render()
    expect(document.body.textContent).not.toContain(creation)
    expect(document.querySelector('input[type=password]')).toBeNull()
    releasePost!()
    await settle()
    fresh = true
    await render()
    expect(document.body.textContent).toContain('unconfirmed')
    status = 200
    postGate = undefined
    await click('Reconcile exact command')
    expect(posts()[1].data).toBe(
      JSON.stringify({
        request_id: JSON.parse(original.data).request_id,
        reason: 'Exact orphan cleanup',
      }),
    )
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  },
)
it.each(['orphan', 'committed'])(
  'queued confirmation after current permission revocation sends nothing and clears Token (%s)',
  async (state) => {
    value.state = state
    await render()
    await review()
    const queued = buttons('Confirm cleanup')[0]
    permissions = ['secrets.read']
    await act(async () =>
      cache.invalidateQueries({ queryKey: ['permissions', actor, 'provider-orphans'] }),
    )
    await act(async () => queued.click())
    await settle()
    expect(posts()).toHaveLength(0)
    expect(document.querySelector('input[type=password]')).toBeNull()
  },
)
it.each(['orphan', 'committed'])(
  'actor replacement hides old draft and rejects late held response (%s)',
  async (state) => {
    value.state = state
    await render()
    await review()
    postGate = new Promise((done) => {
      releasePost = done
    })
    await act(async () => buttons('Confirm cleanup')[0].click())
    await settle()
    actor = 'usr_replacement'
    await act(async () =>
      cache.setQueryData(sessionKey, {
        user: { id: actor, role: 'admin' },
        csrf_token: 'd'.repeat(64),
      }),
    )
    await render()
    releasePost!()
    await settle()
    expect(document.body.textContent).not.toContain('Exact orphan cleanup')
    expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
    expect(posts()).toHaveLength(1)
  },
)
it('cancellation retains command uncertainty and unmount ignores late response', async () => {
  await render()
  await review()
  postGate = new Promise((done) => {
    releasePost = done
  })
  await act(async () => buttons('Confirm cleanup')[0].click())
  await settle()
  await click('Cancel waiting')
  expect(document.body.textContent).toContain('unconfirmed')
  await act(async () => root.render(<span>closed</span>))
  releasePost!()
  await settle()
  expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
  expect(posts()).toHaveLength(1)
})
it('fresh review changed identity hides prior submitted intent instead of relabeling success', async () => {
  status = 503
  await render()
  await review()
  await click('Confirm cleanup')
  value.review_etag = 'd'.repeat(64) + '.' + 'b'.repeat(64)
  await click('Review current status', 1)
  expect(document.body.textContent).not.toContain('Exact orphan cleanup')
  expect(buttons('Reconcile exact command')).toHaveLength(0)
  expect(posts()).toHaveLength(1)
})

it('an eligible detail reply arriving after actor replacement cannot open its confirmation', async () => {
  await render()
  await click('Review current status')
  await input('Reason', 'Old actor intent')
  detailGate = new Promise((done) => {
    releaseDetail = done
  })
  await act(async () => buttons('Review cleanup')[0].click())
  actor = 'usr_replacement'
  await act(async () =>
    cache.setQueryData(sessionKey, {
      user: { id: actor, role: 'admin' },
      csrf_token: 'd'.repeat(64),
    }),
  )
  await render()
  releaseDetail!()
  await settle()
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(document.body.textContent).not.toContain('Old actor intent')
  expect(posts()).toHaveLength(0)
})
it('renewed detail error hides private facts and clears a queued transient Token', async () => {
  await render()
  await review()
  const queued = buttons('Confirm cleanup')[0]
  await act(async () =>
    cache.setQueryData(
      ['admin', 'provider-orphan', actor, integration, generation, creation],
      undefined,
    ),
  )
  await act(async () =>
    cache.removeQueries({
      queryKey: ['admin', 'provider-orphan', actor, integration, generation, creation],
      exact: true,
    }),
  )
  fresh = false
  await render()
  await act(async () => queued.click())
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(document.body.textContent).not.toContain(creation)
  expect(posts()).toHaveLength(0)
})
it('current-process ownership unknown blocks even original successful Write and Read', async () => {
  value.eligible = false
  value.blocker_codes = ['process_ownership_unknown']
  await render()
  await click('Review current status')
  expect(document.body.textContent).toContain('not exclusively owned by this process generation')
  expect(buttons('Review cleanup')[0].disabled).toBe(true)
  expect(posts()).toHaveLength(0)
})

it('intrinsic non-administrator does not fetch permissions or private orphan facts', async () => {
  admin = false
  await render()
  expect(requests).toHaveLength(0)
  expect(document.body.textContent).not.toContain(creation)
  expect(posts()).toHaveLength(0)
})
it('mismatched fresh detail response ETag prevents danger confirmation', async () => {
  await render()
  await click('Review current status')
  await input('Reason', 'Exact review')
  detailHeader = '"' + 'e'.repeat(64) + '.' + 'b'.repeat(64) + '"'
  await click('Review cleanup')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(document.body.textContent).not.toContain(creation + ' ·')
  expect(posts()).toHaveLength(0)
})

it.each(['orphan', 'committed'])(
  'Integration target replacement destroys the old intent and ignores its late response (%s)',
  async (state) => {
    value.state = state
    await render()
    await review()
    postGate = new Promise((done) => {
      releasePost = done
    })
    await act(async () => buttons('Confirm cleanup')[0].click())
    await settle()
    targetIntegration = 'vlt_02j00000000000000000000000'
    value.integration_id = targetIntegration
    value.creation_request_id = '33333333-3333-4333-8333-333333333333'
    await render()
    releasePost!()
    await settle()
    expect(document.body.textContent).not.toContain('Exact orphan cleanup')
    expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
    expect(buttons('Reconcile exact command')).toHaveLength(0)
    expect(document.querySelector('input[type=password]')).toBeNull()
    expect(
      requests.some((r) => r.url?.includes('/' + targetIntegration + '/provider-orphans')),
    ).toBe(true)
    expect(posts()).toHaveLength(1)
  },
)

async function failedPublishedCommand() {
  value.state = 'committed'
  commandState = 'failed'
  await render()
  await review()
  await click('Confirm cleanup')
  return JSON.parse(posts()[0].data) as {
    request_id: string
    reason: string
    cleanup_token: string
  }
}
it('failed published command requires explicit current eligibility before any new confirmation', async () => {
  const original = await failedPublishedCommand()
  value.eligible = false
  value.blocker_codes = ['published_drain_unproven']
  await click('Review a new cleanup attempt')
  expect(document.body.textContent).toContain('current review does not permit a new command')
  expect(document.body.textContent).toContain(original.request_id)
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(posts()).toHaveLength(1)
  await click('Review recorded command')
  await click('Reconcile exact command')
  expect(JSON.parse(posts()[1].data)).toEqual({
    request_id: original.request_id,
    reason: original.reason,
  })
  expect(document.body.textContent).toContain('recorded a failure')
})
it('new eligible review needs fresh reason and confirmation, keeps old receipt and creates a different UUID', async () => {
  const original = await failedPublishedCommand()
  value.review_etag = 'a'.repeat(64) + '.' + 'd'.repeat(64)
  await click('Review a new cleanup attempt')
  expect(document.body.textContent).toContain('original failed receipt remains retained')
  expect(document.body.textContent).toContain(original.request_id)
  expect(posts()).toHaveLength(1)
  await input('Independent cleanup Token', 'fresh-cleanup-secret')
  expect(buttons('Confirm new cleanup attempt')[0].disabled).toBe(true)
  await input('Reason for the new command', 'Joined current published object')
  commandState = 'acknowledged'
  await click('Confirm new cleanup attempt')
  expect(posts()).toHaveLength(2)
  const next = JSON.parse(posts()[1].data)
  expect(next.request_id).not.toBe(original.request_id)
  expect(next.reason).toBe('Joined current published object')
  expect(next.cleanup_token).toBe('fresh-cleanup-secret')
  expect(posts()[1].headers.get('If-Match')).toBe('"' + value.review_etag + '"')
  expect(document.body.textContent).toContain('Previous failed command')
  expect(document.body.textContent).toContain(original.request_id)
  expect(document.body.textContent).toContain(original.reason)
  await click('Review previous command receipt')
  expect(posts()).toHaveLength(2)
  expect(requests.at(-1)?.url).toContain('/commands/' + original.request_id)
  expect(document.body.textContent).toContain('recorded a failure')
  expect(commands.get(original.request_id)?.state).toBe('failed')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state),
    ),
  ).not.toContain('fresh-cleanup-secret')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it.each(['pending', 'unknown'])(
  'original %s never becomes a new-intent action from an eligible preview',
  async (state) => {
    value.state = 'committed'
    commandState = state
    status = state === 'pending' ? 202 : 200
    await render()
    await review()
    await click('Confirm cleanup')
    await click('Review current status', 1)
    expect(buttons('Review a new cleanup attempt')).toHaveLength(0)
    expect(buttons('Confirm new cleanup attempt')).toHaveLength(0)
    expect(posts()).toHaveLength(1)
  },
)
it('failed current review retains original intent for receipt reconciliation after authorized refresh', async () => {
  const original = await failedPublishedCommand()
  detailStatus = 503
  await click('Review a new cleanup attempt')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(posts()).toHaveLength(1)
  detailStatus = 200
  await click('Review current status')
  expect(document.body.textContent).toContain(original.request_id)
  await click('Review recorded command')
  expect(posts()).toHaveLength(1)
  await click('Reconcile exact command')
  expect(JSON.parse(posts()[1].data).request_id).toBe(original.request_id)
  expect(JSON.parse(posts()[1].data)).not.toHaveProperty('cleanup_token')
})
it('closing a new confirmation keeps the original command and requires another explicit fresh review', async () => {
  const original = await failedPublishedCommand()
  await click('Review a new cleanup attempt')
  await input('Reason for the new command', 'Draft new reason')
  await input('Independent cleanup Token', 'new-transient-token')
  await click('Close')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(posts()).toHaveLength(1)
  await click('Reconcile exact command')
  expect(JSON.parse(posts()[1].data).request_id).toBe(original.request_id)
  expect(JSON.parse(posts()[1].data).reason).toBe(original.reason)
  await click('Review a new cleanup attempt')
  expect(document.querySelector<HTMLInputElement>('input[type=password]')?.value).toBe('')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].find((e) =>
      e.parentElement?.textContent?.includes('Reason for the new command'),
    )?.value,
  ).toBe('')
})
it('new confirmation renewed by the Session cannot dispatch, and original receipt remains', async () => {
  const original = await failedPublishedCommand()
  await click('Review a new cleanup attempt')
  await input('Reason for the new command', 'New reason before renewal')
  await input('Independent cleanup Token', 'new-token')
  const queued = buttons('Confirm new cleanup attempt')[0]
  fresh = false
  generation++
  await render()
  await act(async () => queued.click())
  fresh = true
  await render()
  expect(posts()).toHaveLength(1)
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(document.body.textContent).toContain(original.request_id)
  await click('Reconcile exact command')
  expect(JSON.parse(posts()[1].data).request_id).toBe(original.request_id)
})
it('held new review cannot restore confirmation after same-owner Session renewal', async () => {
  const original = await failedPublishedCommand()
  detailGate = new Promise((done) => {
    releaseDetail = done
  })
  await act(async () => buttons('Review a new cleanup attempt')[0].click())
  fresh = false
  generation++
  await render()
  releaseDetail!()
  await settle()
  detailGate = undefined
  fresh = true
  await render()
  expect(posts()).toHaveLength(1)
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(document.body.textContent).toContain(original.request_id)
})
it('actor replacement discards new confirmation and previous private command facts', async () => {
  const original = await failedPublishedCommand()
  await click('Review a new cleanup attempt')
  await input('Reason for the new command', 'Previous actor new reason')
  await input('Independent cleanup Token', 'new-token')
  const queued = buttons('Confirm new cleanup attempt')[0]
  actor = 'usr_replacement'
  await act(async () =>
    cache.setQueryData(sessionKey, {
      user: { id: actor, role: 'admin' },
      csrf_token: 'd'.repeat(64),
    }),
  )
  await render()
  await act(async () => queued.click())
  expect(document.body.textContent).not.toContain(original.request_id)
  expect(document.body.textContent).not.toContain('Previous actor new reason')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(posts()).toHaveLength(1)
})
it('new dispatch uncertainty retains both original failed and new command identities without automatic retry', async () => {
  const original = await failedPublishedCommand()
  await click('Review a new cleanup attempt')
  await input('Reason for the new command', 'Reviewed next uncertain command')
  await input('Independent cleanup Token', 'new-token')
  status = 503
  await click('Confirm new cleanup attempt')
  const next = JSON.parse(posts()[1].data)
  expect(next.request_id).not.toBe(original.request_id)
  expect(document.body.textContent).toContain(original.request_id)
  expect(document.body.textContent).toContain('unconfirmed')
  expect(buttons('Review a new cleanup attempt')).toHaveLength(0)
  await settle()
  expect(posts()).toHaveLength(2)
  expect(document.querySelector('input[type=password]')).toBeNull()
})
it('new review and confirmation copy switch EN/ZH while original receipt remains visible', async () => {
  const original = await failedPublishedCommand()
  await click('Review a new cleanup attempt')
  await input('Reason for the new command', 'New explicit reason')
  await input('Independent cleanup Token', 'new-token')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('原始失败收据保持不变')
  expect(document.body.textContent).toContain('仅有失败或超时并不允许再次尝试')
  expect(document.body.textContent).toContain(original.request_id)
  expect(document.querySelector<HTMLInputElement>('input[type=password]')?.value).toBe('new-token')
  expect(posts()).toHaveLength(1)
})
it('canceling a held new review cannot open a new confirmation from its late eligible reply', async () => {
  const original = await failedPublishedCommand()
  detailGate = new Promise((done) => {
    releaseDetail = done
  })
  await act(async () => buttons('Review a new cleanup attempt')[0].click())
  await settle()
  await click('Cancel waiting')
  releaseDetail!()
  await settle()
  detailGate = undefined
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(buttons('Confirm new cleanup attempt')).toHaveLength(0)
  expect(posts()).toHaveLength(1)
  await click('Review current status')
  expect(document.body.textContent).toContain(original.request_id)
})
it('a failed receipt without a terminal finish cannot offer a new command', async () => {
  const original = await failedPublishedCommand()
  const recorded = commands.get(original.request_id)!
  commands.set(original.request_id, { ...recorded, finished_at: null })
  await click('Review recorded command')
  expect(buttons('Review a new cleanup attempt')).toHaveLength(0)
  expect(posts()).toHaveLength(1)
})
it('new review requires independent current write permissions and preserves original receipt reads', async () => {
  const original = await failedPublishedCommand()
  permissions = ['secrets.read', 'secrets.write']
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['permissions', actor, 'provider-orphans'] }),
  )
  await settle()
  expect(buttons('Review a new cleanup attempt')[0].disabled).toBe(true)
  await click('Review recorded command')
  expect(document.body.textContent).toContain(original.request_id)
  expect(posts()).toHaveLength(1)
})
