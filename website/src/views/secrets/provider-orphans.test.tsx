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
  status: number
let postGate: Promise<void> | undefined,
  releasePost: (() => void) | undefined,
  detailGate: Promise<void> | undefined,
  releaseDetail: (() => void) | undefined
let commandState = 'acknowledged',
  commandGETStatus = 200
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
    finished_at: commandState === 'acknowledged' ? '2026-10-07T01:00:01Z' : null,
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
      return response(
        {
          receipt: receipt(JSON.parse(config.data).request_id),
          running: commandState === 'pending',
        },
        status,
      )
    }
    if (config.url?.includes('/commands/')) {
      if (commandGETStatus !== 200)
        throw new AxiosError('hidden', undefined, config, undefined, response({}, commandGETStatus))
      return response(receipt(config.url.split('/').at(-1)!))
    }
    if (config.url?.includes('?limit=20'))
      return response({ items: [structuredClone(value)], next_cursor: null })
    const captured = structuredClone(value)
    if (detailGate) await detailGate
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
it.each([200, 503])(
  'held dispatched result %s during Session renewal preserves uncertain intent and releases busy',
  async (result) => {
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
it('queued confirmation after current permission revocation sends nothing and clears Token', async () => {
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
})
it('actor replacement hides old draft and rejects late held response', async () => {
  await render()
  await review()
  postGate = new Promise((done) => {
    releasePost = done
  })
  await act(async () => buttons('Confirm cleanup')[0].click())
  await settle()
  actor = 'usr_replacement'
  cache.setQueryData(sessionKey, { user: { id: actor, role: 'admin' }, csrf_token: 'd'.repeat(64) })
  await render()
  releasePost!()
  await settle()
  expect(document.body.textContent).not.toContain('Exact orphan cleanup')
  expect(document.body.textContent).not.toContain('destroy acknowledged and recorded')
  expect(posts()).toHaveLength(1)
})
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

it('Integration target replacement destroys the old intent and ignores its late response', async () => {
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
  expect(requests.some((r) => r.url?.includes('/' + targetIntegration + '/provider-orphans'))).toBe(
    true,
  )
  expect(posts()).toHaveLength(1)
})
