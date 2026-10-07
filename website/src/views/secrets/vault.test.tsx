import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import SecretStorePage from './index'
import { store } from './fixtures'
import { probe, vault, vaultETag, vaultID } from './vault-fixtures'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  oldAdapter: typeof client.defaults.adapter,
  session: Session,
  permissions: string[],
  reads: string[],
  writes: InternalAxiosRequestConfig[],
  getStatus: number,
  readGate: Promise<void> | undefined
let listingMethod: 'token' | 'approle' = 'token'
let listingProbe = false
let listingRevision: string | undefined
let detailValue = vault()
let detailGate: Promise<void> | undefined
let listGate: Promise<void> | undefined
let resolveList: (() => void) | undefined
let resolveDetail: (() => void) | undefined
let resolveRead: (() => void) | undefined
const settle = () =>
  act(async () => {
    await new Promise((done) => setTimeout(done, 35))
  })
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
const menuItem = (label: string) =>
  [...document.querySelectorAll<HTMLElement>('[role=menuitem]')].find(
    (item) => item.textContent === label,
  )
async function openMenu() {
  if (!document.querySelector('[role=menu]')) {
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[aria-label="More actions for QA Vault"]')!
        .click(),
    )
    await settle()
  }
}
async function click(label: string) {
  if (!button(label) && !menuItem(label)) await openMenu()
  await act(async () => (button(label) ?? menuItem(label))!.click())
  await settle()
}
async function mount(path = '/admin/secrets?tab=vault&keep=1') {
  cache.setQueryData(sessionKey, session)
  router = createMemoryRouter(
    [
      { path: '/admin/secrets', element: <SecretStorePage /> },
      { path: '/admin/secrets/rotations/:rotationId', element: <SecretStorePage /> },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  session = {
    user: { id: 'usr_admin', name: 'QA', email: 'qa@example.test', role: 'admin' },
    csrf_token: 'c'.repeat(64),
  }
  permissions = ['secrets.read', 'secrets.write', 'secrets.test']
  reads = []
  writes = []
  getStatus = 200
  listingMethod = 'token'
  listingProbe = false
  listingRevision = undefined
  detailValue = vault()
  detailGate = undefined
  listGate = undefined
  resolveList = undefined
  resolveDetail = undefined
  readGate = undefined
  resolveRead = undefined
  oldAdapter = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    const path = config.url!
    const response = (data: unknown, status = 200) => ({
      data,
      status,
      statusText: String(status),
      headers: { etag: `"${(data as { review_etag?: string })?.review_etag ?? vaultETag}"` },
      config,
    })
    if (config.method === 'get') {
      reads.push(path)
      if (path === '/auth/session') return response(session)
      if (path === '/auth/permissions') return response({ permissions: [...permissions] })
      const captured = vault()
      captured.writer_auth.method = listingMethod
      if (listingProbe) captured.last_probe = probe()
      if (listingRevision) captured.revision_id = listingRevision
      if (readGate) await readGate
      if (getStatus !== 200)
        throw new AxiosError('hidden', undefined, config, undefined, response({}, getStatus))
      if (path.includes('/provider-orphans?')) return response({ items: [], next_cursor: null })
      if (path.includes('/probes/')) return response(probe())
      if (path === `/admin/secrets/integrations/${vaultID}`) {
        const value = structuredClone(detailValue)
        if (detailGate) await detailGate
        return response(value)
      }
      if (path.startsWith('/admin/secrets/integrations?')) {
        if (listGate) await listGate
        return response({
          items: [captured],
          next_cursor: null,
          review_etag: vaultETag,
          can_write: true,
          can_test: true,
        })
      }
      return response(store())
    }
    writes.push(config)
    const result = probe()
    if (path.endsWith('/write')) result.request_id = JSON.parse(config.data).request_id
    return response(result)
  }
})
afterEach(async () => {
  resolveRead?.()
  resolveDetail?.()
  resolveList?.()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
it('Vault-only tab makes no root inventory request and preserves search parameters on real tab navigation', async () => {
  await mount()
  expect(document.body.textContent).toContain('QA Vault')
  expect(reads.some((path) => path === '/admin/secrets' || path.includes('/store'))).toBe(false)
  expect(document.querySelector('input[type=password]')).toBeNull()
  await click('Storage')
  expect(router.state.location.search).toContain('keep=1')
  expect(router.state.location.search).toContain('tab=storage')
  expect(document.body.textContent).toContain('Internal Secret Store')
  expect(reads).toContain('/admin/secrets')
})
it('default and rotation routes stay root-only even with a Vault query parameter', async () => {
  await mount('/admin/secrets/rotations/srt_01k00000000000000000000000?tab=vault')
  expect(reads.some((path) => path.includes('/integrations'))).toBe(false)
  expect(document.body.textContent).not.toContain('Add integration')
})
it('independent reader authority shows configuration but no save or test dispatch', async () => {
  permissions = ['secrets.read']
  await mount()
  expect(button('Add integration').disabled).toBe(true)
  await openMenu()
  expect(menuItem('Write test')?.getAttribute('aria-disabled')).toBe('true')
  await click('Integration configuration')
  expect(document.body.textContent).toContain(
    'Write permission and current server editability are required.',
  )
  expect(button('Save configuration').disabled).toBe(true)
  expect(writes).toHaveLength(0)
})
it('write permission does not grant test or rotation authority', async () => {
  permissions = ['secrets.read', 'secrets.write']
  await mount()
  expect(button('Add integration').disabled).toBe(false)
  await openMenu()
  expect(menuItem('Write test')?.getAttribute('aria-disabled')).toBe('true')
  expect(reads.some((path) => path.includes('/rotations'))).toBe(false)
})
it('test permission does not grant configuration write authority', async () => {
  permissions = ['secrets.read', 'secrets.test']
  await mount()
  expect(button('Add integration').disabled).toBe(true)
  await openMenu()
  expect(menuItem('Write test')?.getAttribute('aria-disabled')).not.toBe('true')
})
it('live language change preserves selected table and replaces visible/accessibility labels', async () => {
  await mount()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('添加集成')
  expect(document.body.textContent).toContain('写入身份')
  expect(document.body.textContent).toContain('QA Vault')
  expect(writes).toHaveLength(0)
})
it('renewed list authority hides cached rows and late old reads cannot restore them after actor change', async () => {
  await mount()
  readGate = new Promise((done) => {
    resolveRead = done
  })
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'vault'] })
  })
  await settle()
  expect(document.body.textContent).not.toContain('QA Vault')
  session = { ...session, user: { ...session.user, id: 'usr_other', role: 'member' } }
  await act(async () => cache.setQueryData(sessionKey, session))
  resolveRead!()
  await settle()
  expect(document.body.textContent).not.toContain('QA Vault')
  expect(writes).toHaveLength(0)
})
it('errors hide old rows and empty stale caches never authorize actions', async () => {
  await mount()
  getStatus = 503
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'vault'] })
  })
  await settle()
  expect(document.body.textContent).not.toContain('QA Vault')
  expect(button('Add integration')).toBeUndefined()
  expect(document.body.textContent).toContain('could not be confirmed')
  expect(writes).toHaveLength(0)
})
it('read denial never fetches integration or root inventory', async () => {
  permissions = ['secrets.write', 'secrets.test']
  await mount()
  expect(reads.filter((path) => path.startsWith('/admin/'))).toEqual([])
  expect(reads).toContain('/auth/permissions')
  expect(document.body.textContent).toContain('read permission are required')
})

it('one explicit Write saves awaiting-Read facts without a second HTTP command or automatic Read', async () => {
  await mount()
  await click('Write test')
  const node = [...document.querySelectorAll('label')]
    .find((item) => item.textContent?.startsWith('Reason'))!
    .querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      node,
      'Reviewed probe',
    )
    node.dispatchEvent(new Event('input', { bubbles: true }))
    node.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await click('Review command')
  await click('Confirm action')
  expect(writes).toHaveLength(1)
  expect(writes[0].url).toBe(`/admin/secrets/integrations/${vaultID}/probes/write`)
  expect(document.body.textContent).toContain('Awaiting Read')
  expect(document.body.textContent).toContain('Not attempted')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('fresh Session fetch hides open secret form and no cached authority permits dispatch', async () => {
  await mount()
  await click('Add integration')
  expect(document.querySelectorAll('input[type=password]')).toHaveLength(2)
  readGate = new Promise((done) => {
    resolveRead = done
  })
  const previous = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    if (config.url === '/auth/session') {
      await readGate
      return { data: session, status: 200, statusText: '200', headers: {}, config }
    }
    return (previous as (c: InternalAxiosRequestConfig) => Promise<unknown>)(config) as never
  }
  await act(async () => {
    void cache.refetchQueries({ queryKey: sessionKey })
  })
  await settle()
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(writes).toHaveLength(0)
  resolveRead!()
  await settle()
  expect(writes).toHaveLength(0)
})

it('explicit Session expiry immediately destroys the form before stale confirmation can dispatch', async () => {
  await mount()
  await click('Add integration')
  await act(async () => {
    window.dispatchEvent(new Event('routex:session-expired'))
    button('Save configuration')?.click()
  })
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(writes).toHaveLength(0)
  await act(async () => cache.setQueryData(sessionKey, session))
  await settle()
  expect(document.body.textContent).not.toContain('QA Vault')
  expect(writes).toHaveLength(0)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('a reader can inspect saved stage facts through GET without probe or write authority', async () => {
  listingProbe = true
  permissions = ['secrets.read']
  await mount()
  await click('View saved facts')
  expect(document.body.textContent).toContain('Saved probe observations')
  expect(document.body.textContent).toContain('0.123456')
  expect(reads).toContain(`/admin/secrets/integrations/${vaultID}/probes/${probe().id}`)
  expect(button('Review command')).toBeUndefined()
  expect(document.querySelector('input')).toBeNull()
  expect(writes).toHaveLength(0)
})

async function changeInput(label: string, value: string) {
  const node = [...document.querySelectorAll('label')]
    .find((item) => item.textContent?.startsWith(label))!
    .querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
    node.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
it('waits for delayed exact detail and seeds its descriptor/name/auth together with its own review', async () => {
  await mount()
  detailValue = {
    ...vault(),
    name: 'New exact name',
    descriptor: { ...vault().descriptor, namespace: 'changed', mount: 'changed-mount' },
    reader_auth: { method: 'token', configured: false },
    review_etag: 'd'.repeat(64) + '.' + 'e'.repeat(64),
  }
  detailGate = new Promise((done) => {
    resolveDetail = done
  })
  await click('Integration configuration')
  expect(document.querySelector('[role=dialog]')).toBeNull()
  resolveDetail!()
  await settle()
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('New exact name')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('changed-mount')
  expect(document.querySelectorAll('input[type=password]')).toHaveLength(1)
  expect(button('Save configuration').disabled).toBe(true)
  expect(writes).toHaveLength(0)
})
it('later fresh detail keeps the initialized draft and Tokens, blocks a newer unreviewed ETag, and requires explicit review', async () => {
  await mount()
  await click('Integration configuration')
  await changeInput('Integration name', 'Exact human draft')
  await changeInput('Reason', 'Preserved reason')
  detailValue = {
    ...vault(),
    name: 'Later server name',
    review_etag: 'd'.repeat(64) + '.' + 'e'.repeat(64),
  }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'vault-detail'] })
  })
  await settle()
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('Exact human draft')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).not.toContain('Later server name')
  expect(button('Save configuration').disabled).toBe(true)
  await click('Review current status')
  expect(button('Save configuration').disabled).toBe(false)
  expect(writes).toHaveLength(0)
})
it('uses five compact columns, namespace/field composite cells, one row menu and centered add beneath the table', async () => {
  await mount()
  const table = document.querySelector('table')!
  expect([...table.querySelectorAll('thead th')].map((node) => node.textContent)).toEqual([
    'Integration',
    'Vault location',
    'Authentication and links',
    'Usage',
    'Actions',
  ])
  expect(table.textContent).toContain('Namespace (optional): acme')
  expect(table.textContent).toContain('Data field: value')
  expect(table.querySelectorAll('tbody button')).toHaveLength(1)
  expect(table.querySelector('button')?.getAttribute('aria-label')).toBe(
    'More actions for QA Vault',
  )
  expect(button('Add integration').parentElement?.className).toContain('justify-center')
  expect(
    table.compareDocumentPosition(button('Add integration')) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).not.toBe(0)
  await openMenu()
  expect(menuItem('Integration configuration')).toBeDefined()
  await act(async () =>
    document
      .querySelector('[role=menu]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await settle()
  expect(document.querySelector('[role=menu]')).toBeNull()
  expect(writes).toHaveLength(0)
})

it('retains successful exact detail during a held list renewal and initializes once after fresh authority returns', async () => {
  await mount()
  detailValue = {
    ...vault(),
    name: 'Exact fast detail',
    review_etag: 'd'.repeat(64) + '.' + 'e'.repeat(64),
  }
  detailGate = new Promise((done) => {
    resolveDetail = done
  })
  await click('Integration configuration')
  listGate = new Promise((done) => {
    resolveList = done
  })
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'vault'] })
  })
  await settle()
  resolveDetail!()
  await settle()
  expect(document.querySelector('[role=dialog]')).toBeNull()
  const detailReads = reads.filter(
    (path) => path === `/admin/secrets/integrations/${vaultID}`,
  ).length
  resolveList!()
  await settle()
  expect(document.querySelector('[role=dialog]')).not.toBeNull()
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('Exact fast detail')
  expect(reads.filter((path) => path === `/admin/secrets/integrations/${vaultID}`)).toHaveLength(
    detailReads,
  )
  await changeInput('Integration name', 'Preserved held-renewal draft')
  detailValue = { ...vault(), name: 'Later detail' }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'vault-detail'] })
  })
  await settle()
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('Preserved held-renewal draft')
  expect(writes).toHaveLength(0)
})
it('manual cached detail cannot replace a retained successful response while fresh list authority is pending', async () => {
  await mount()
  detailGate = new Promise((done) => {
    resolveDetail = done
  })
  await click('Integration configuration')
  listGate = new Promise((done) => {
    resolveList = done
  })
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'vault'] })
  })
  resolveDetail!()
  await settle()
  await act(async () => {
    for (const [key] of cache.getQueriesData({ queryKey: ['admin', 'vault-detail'] }))
      cache.setQueryData(key, { ...vault(), name: 'Unreviewed cached detail' })
  })
  resolveList!()
  await settle()
  expect(document.querySelector('[role=dialog]')).toBeNull()
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'vault-detail'] })
  })
  await settle()
  expect(document.querySelector('[role=dialog]')).not.toBeNull()
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).not.toContain('Unreviewed cached detail')
  expect(writes).toHaveLength(0)
})

it('shows retained old-revision probe facts as historical, blocks Read and retains separate saved facts and Cleanup', async () => {
  listingProbe = true
  listingRevision = 'vlr_01k00000000000000000000001'
  detailValue = { ...vault(), revision_id: listingRevision, last_probe: probe() }
  await mount()
  expect(document.querySelector('table')?.textContent).toContain('Historical revision observations')
  await openMenu()
  expect(menuItem('Read test')?.getAttribute('aria-disabled')).toBe('true')
  expect(menuItem('Clean up owned probe')).toBeDefined()
  expect(menuItem('Clean up owned probe')?.getAttribute('aria-disabled')).not.toBe('true')
  await click('View saved facts')
  expect(document.body.textContent).toContain(probe().id)
  expect(document.body.textContent).toContain('Historical revision observations')
  expect(writes).toHaveLength(0)
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('历史修订的观测结果')
})

it('read-only listing shows recorded AppRole and Token methods without secret inputs or write authority', async () => {
  listingMethod = 'approle'
  permissions = ['secrets.read']
  await mount()
  const rows = [...document.querySelectorAll('tbody tr')]
  expect(rows).toHaveLength(1)
  expect(rows[0].textContent).toContain('AppRole')
  expect(rows[0].textContent).toContain('Token')
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(button('Add integration').disabled).toBe(true)
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(rows[0].textContent).toContain('AppRole')
  expect(writes).toHaveLength(0)
})

it('opens the scoped Provider orphan table from the existing Vault menu with read-only authority', async () => {
  permissions = ['secrets.read']
  await mount()
  await click('Provider orphan cleanup')
  expect(document.body.textContent).toContain('No retained creation records on this page.')
  expect(reads).toContain(`/admin/secrets/integrations/${vaultID}/provider-orphans?limit=20`)
  expect(reads.some((path) => path.includes('/rotations') || path.includes('/roots'))).toBe(false)
  expect(writes).toHaveLength(0)
})
it('queued orphan menu action after read revocation cannot start a private preview', async () => {
  await mount()
  await openMenu()
  const queued = menuItem('Provider orphan cleanup')!
  permissions = []
  await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
  await act(async () => queued.click())
  await settle()
  expect(reads.some((path) => path.includes('/provider-orphans'))).toBe(false)
  expect(writes).toHaveLength(0)
})
