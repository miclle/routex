import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Provider } from '@/types/catalog'
import ConnectionTester from './connection-test'
import { Button } from '@/components/ui/button'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const oldAdapter = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const permissionKey = ['permissions', 'usr_one', 'test', 1]
const catalogueKey = ['admin', 'providers', 'usr_one', 'test', 1]
const manage = vi.fn()
let host: HTMLDivElement, root: Root, cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let release: (() => void) | undefined, hold: Promise<void> | undefined
let testStatus: number,
  resultScope: string,
  resultOutcome: string,
  count: number | null,
  metadataStatus: number
let signal: AbortSignal | undefined
let sessionGeneration: number
let adapter: 'native' | 'azure_openai_classic'
let target = 'con_one'
let propsActor = 'usr_one'
let lockedCredential: string | undefined
const providers = (): Provider[] => [
  {
    id: 'prv_one',
    name: 'Provider',
    connections: [
      {
        id: 'con_one',
        name: 'Stored connection',
        base_url: 'https://upstream.invalid',
        protocol: 'openai_chat',
        enabled: false,
        provider_models: [],
        credentials: [
          {
            id: 'crd_first',
            name: 'First',
            priority: 100,
            enabled: true,
            verification_status: 'verified',
            verified_at: null,
          },
          {
            id: 'crd_selected',
            name: 'Explicit pending',
            priority: 0,
            enabled: false,
            verification_status: 'pending',
            verified_at: null,
          },
        ],
      },
    ],
  },
]
function Host() {
  const [open, setOpen] = useState(true)
  return open ? (
    <ConnectionTester
      actor={propsActor}
      providerId="prv_one"
      connectionId={target}
      credentialId={lockedCredential}
      generation={sessionGeneration}
      permissionsKey={permissionKey}
      catalogueKey={catalogueKey}
      onClose={() => setOpen(false)}
      onManage={manage}
      finalFocus={host}
    />
  ) : (
    <p>Closed</p>
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  host.tabIndex = -1
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: 'usr_one', role: 'admin' },
    csrf_token: 'current-csrf',
  })
  sessionGeneration = cache.getQueryState(sessionKey)!.dataUpdateCount
  cache.setQueryData(permissionKey, ['providers.read', 'providers.write'])
  cache.setQueryData(catalogueKey, providers())
  requests = []
  release = undefined
  hold = undefined
  signal = undefined
  testStatus = 200
  metadataStatus = 200
  resultScope = 'model_discovery'
  adapter = 'native'
  resultOutcome = 'passed'
  count = 2
  target = 'con_one'
  propsActor = 'usr_one'
  lockedCredential = undefined
  manage.mockClear()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'Cache-Control': 'private,no-store' }),
      data: {} as unknown,
    }
    if (config.url?.endsWith('/metadata')) {
      if (metadataStatus !== 200)
        throw new AxiosError('secret source details', '', config, undefined, {
          ...response,
          status: metadataStatus,
        })
      response.headers.set('ETag', `"${token}"`)
      response.data = {
        id: 'con_one',
        provider_id: 'prv_one',
        name: 'Stored connection',
        adapter,
        api_version: adapter === 'native' ? null : '2024-10-21',
        protocol: 'openai_chat',
        base_url: 'https://upstream.invalid',
        egress_mode: 'direct',
        egress_id: null,
        etag: token,
        can_edit: true,
        transport_generation: '0',
        can_edit_transport: true,
        transport_locked: false,
      }
    } else if (config.url?.endsWith('/test')) {
      signal = config.signal as AbortSignal
      if (hold) await hold
      if (testStatus !== 200)
        throw new AxiosError('secret source details', '', config, undefined, {
          ...response,
          status: testStatus,
          data: { message: 'secret source details' },
        })
      response.data = {
        connection_id: 'con_one',
        credential_id: JSON.parse(config.data).credential_id,
        outcome: resultOutcome,
        scope: resultScope,
        discovered_model_count: count,
        checked_at: '2026-10-09T01:02:03.123456789Z',
      }
    } else throw new Error('Unexpected request')
    return response
  }
})
afterEach(async () => {
  release?.()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  await i18n.changeLanguage('en')
  vi.restoreAllMocks()
})
async function until(assertion: () => void) {
  for (let i = 0; i < 150; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 149) throw error
    }
  }
}
const button = (label: string) =>
  [...document.querySelectorAll<HTMLElement>('button,[role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
async function click(label: string) {
  expect(button(label)).toBeTruthy()
  await act(async () => button(label).click())
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(button('Credential to test')).toBeTruthy())
}
async function choose() {
  await click('Credential to test')
  await until(() => expect(document.body.textContent).toContain('Explicit pending · crd_selected'))
  const option = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
    row.textContent?.includes('crd_selected'),
  )!
  await act(async () => option.click())
}
const posts = () => requests.filter((request) => request.method === 'post')
function captureRun() {
  interface Fiber {
    type: unknown
    return: Fiber | null
    memoizedProps: { onClick?: () => void }
  }
  const control = button('Run new test')
  const key = Object.keys(control).find((value) => value.startsWith('__reactFiber$'))!
  let fiber = (control as unknown as Record<string, Fiber>)[key]
  while (fiber && fiber.type !== Button) fiber = fiber.return!
  expect(fiber?.memoizedProps.onClick).toBeTypeOf('function')
  return fiber.memoizedProps.onClick!
}
it('reports Azure authentication-only without claiming deployment coverage', async () => {
  adapter = 'azure_openai_classic'
  resultScope = 'authentication_only'
  count = null
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(document.body.textContent).toContain('deployment coverage is not tested')
  expect(document.body.textContent).not.toContain('models discovered')
})
it('rejects a scope that contradicts the reviewed adapter', async () => {
  resultScope = 'authentication_only'
  count = null
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(document.body.textContent).toContain('previous result is unknown'))
  expect(document.body.textContent).not.toContain('This test passed.')
})
it.each(['actor', 'session', 'permission', 'credential', 'expiry'])(
  'synchronously denies captured dispatch after %s change',
  async (boundary) => {
    await mount()
    await choose()
    const invoke = captureRun()
    await act(async () => {
      if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else if (boundary === 'permission') cache.setQueryData(permissionKey, ['providers.read'])
      else if (boundary === 'credential') {
        const rows = providers()
        rows[0].connections[0].credentials = []
        cache.setQueryData(catalogueKey, rows)
      } else
        cache.setQueryData(sessionKey, {
          user: { id: boundary === 'actor' ? 'usr_other' : 'usr_one', role: 'admin' },
          csrf_token: 'renewed',
        })
      invoke()
    })
    expect(posts()).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Explicit pending')
  },
)
it.each(['actor', 'target'])('hides and aborts unkeyed %s change', async (boundary) => {
  hold = new Promise((resolve) => {
    release = resolve
  })
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(signal).toBeTruthy())
  await act(async () => {
    if (boundary === 'actor') propsActor = 'usr_other'
    else target = 'con_other'
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    )
  })
  expect(signal?.aborted).toBe(true)
  await act(async () => release?.())
  expect(document.body.textContent).not.toContain('This test passed.')
  expect(document.body.textContent).not.toContain('Explicit pending')
})
it('aborts and clears held result on reviewed resource renewal', async () => {
  hold = new Promise((resolve) => {
    release = resolve
  })
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(signal).toBeTruthy())
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'connection-test-review'] })
  })
  await until(() => expect(signal?.aborted).toBe(true))
  await act(async () => release?.())
  expect(document.body.textContent).not.toContain('This test passed.')
  expect((button('Run new test') as HTMLButtonElement).disabled).toBe(true)
})
it('requires explicit exact pending/disabled credential, submits once and keeps test facts outside caches', async () => {
  await mount()
  expect((button('Run new test') as HTMLButtonElement).disabled).toBe(true)
  expect(posts()).toHaveLength(0)
  await choose()
  await act(async () => {
    button('Run new test').click()
    button('Run new test').click()
  })
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(posts()).toHaveLength(1)
  expect(JSON.parse(posts()[0].data)).toEqual({ credential_id: 'crd_selected' })
  expect(posts()[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(posts()[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
  expect(document.body.textContent).toContain('2 models discovered')
  expect(document.body.textContent).toContain('Scope: model discovery')
  expect(requests).toHaveLength(2)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain('checked_at')
  expect(window.localStorage.getItem('crd_selected')).toBeNull()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('本次测试通过。')
  expect(document.body.textContent).toContain('发现 2 个模型')
  expect(document.body.textContent).not.toContain('connectionTest.')
})
it('shows real failed test as a result, without inferring verification or readiness', async () => {
  resultOutcome = 'failed'
  count = null
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(document.body.textContent).toContain('This test failed.'))
  expect(document.body.textContent).not.toContain('models discovered')
  expect(posts()).toHaveLength(1)
})
it.each([503, 500])(
  'sanitizes unknown HTTP %s and permits only an explicit new test',
  async (status) => {
    testStatus = status
    await mount()
    await choose()
    await click('Run new test')
    await until(() => expect(document.body.textContent).toContain('previous result is unknown'))
    expect(document.body.textContent).not.toContain('secret source')
    expect(posts()).toHaveLength(1)
    expect(document.body.textContent).not.toContain('This test passed.')
    testStatus = 200
    await click('Run new test')
    await until(() => expect(document.body.textContent).toContain('This test passed.'))
    expect(posts()).toHaveLength(2)
  },
)
it.each([400, 403, 409])(
  'blocks rejected review/authority HTTP %s until explicit fresh authorized review',
  async (status) => {
    testStatus = status
    await mount()
    await choose()
    const obsoleteRun = captureRun()
    await click('Run new test')
    await until(() => expect(button('Refresh authorized facts')).toBeTruthy())
    expect(document.body.textContent).not.toContain('secret source')
    expect((button('Run new test') as HTMLButtonElement).disabled).toBe(true)
    expect(posts()).toHaveLength(1)
    testStatus = 200
    await act(async () => obsoleteRun())
    expect(posts()).toHaveLength(1)
    await click('Run new test')
    expect(posts()).toHaveLength(1)
    await click('Refresh authorized facts')
    expect(document.body.textContent).toContain('Closed')
    expect(posts()).toHaveLength(1)
  },
)
it('cancels and ignores held result on dismissal, returning focus', async () => {
  hold = new Promise((resolve) => {
    release = resolve
  })
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(signal).toBeTruthy())
  await click('Cancel test')
  expect(signal?.aborted).toBe(true)
  await act(async () => release?.())
  expect(document.body.textContent).not.toContain('This test passed.')
  expect(document.body.textContent).toContain('Closed')
  await until(() => expect(document.activeElement).toBe(host))
})
it.each(['session', 'permission', 'catalogue', 'expiry'])(
  'aborts, clears selection and fences late result on %s renewal',
  async (boundary) => {
    hold = new Promise((resolve) => {
      release = resolve
    })
    await mount()
    await choose()
    await click('Run new test')
    await until(() => expect(signal).toBeTruthy())
    await act(async () => {
      if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else
        void cache.invalidateQueries({
          queryKey:
            boundary === 'session'
              ? sessionKey
              : boundary === 'permission'
                ? permissionKey
                : catalogueKey,
        })
    })
    await until(() => expect(signal?.aborted).toBe(true))
    await act(async () => release?.())
    expect(document.body.textContent).not.toContain('Explicit pending')
    expect(document.body.textContent).not.toContain('This test passed.')
    expect(posts()).toHaveLength(1)
  },
)
it('clears sensitive result on unmount without borrowing another target', async () => {
  hold = new Promise((resolve) => {
    release = resolve
  })
  await mount()
  await choose()
  await click('Run new test')
  await until(() => expect(signal).toBeTruthy())
  await act(async () => root.render(<p>Another target</p>))
  expect(signal?.aborted).toBe(true)
  await act(async () => release?.())
  expect(document.body.textContent).toBe('Another target')
})
it('never tests without independent write permission', async () => {
  cache.setQueryData(permissionKey, ['providers.read'])
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(document.body.textContent).toContain('unavailable'))
  expect(button('Run new test')).toBeUndefined()
  expect(posts()).toHaveLength(0)
})
it('guides a connection without credentials to existing management without testing', async () => {
  const rows = providers()
  rows[0].connections[0].credentials = []
  cache.setQueryData(catalogueKey, rows)
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(button('Manage credentials')).toBeTruthy())
  await click('Manage credentials')
  expect(manage).toHaveBeenCalledOnce()
  expect(posts()).toHaveLength(0)
})
it('keeps metadata failure details private and sends no test', async () => {
  metadataStatus = 403
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(document.body.textContent).toContain('unavailable'))
  expect(document.body.textContent).not.toContain('secret source')
  expect(posts()).toHaveLength(0)
})

async function mountLocked() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(document.querySelector('[aria-label="Selected credential"]')).not.toBeNull(),
  )
}
it('locks a row-selected pending disabled Credential without choosing or using a sibling', async () => {
  lockedCredential = 'crd_selected'
  await mountLocked()
  expect(posts()).toHaveLength(0)
  expect(button('Credential to test')).toBeUndefined()
  expect(document.querySelector('[aria-label="Selected credential"]')?.textContent).toContain(
    'Explicit pending · crd_selected',
  )
  await act(async () => {
    button('Run new test').click()
    button('Run new test').click()
  })
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(posts()).toHaveLength(1)
  expect(JSON.parse(posts()[0].data)).toEqual({ credential_id: 'crd_selected' })
  expect(posts()[0].url).toBe('/admin/connections/con_one/test')
  expect(posts()[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(posts()[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('[aria-label="已选择的凭证"]')?.textContent).toContain(
    'crd_selected',
  )
  expect(document.body.textContent).toContain('本次测试仅使用所选凭证')
  expect(document.body.textContent).toContain('本次测试通过。')
})
it.each(['', 'crd_missing', 'crd_other_connection'])(
  'never falls back when the exact row Credential %s is absent from the Connection',
  async (credentialId) => {
    lockedCredential = credentialId
    const rows = providers()
    rows[0].connections.push({
      ...rows[0].connections[0],
      id: 'con_other',
      credentials: [{ ...rows[0].connections[0].credentials[0], id: 'crd_other_connection' }],
    })
    cache.setQueryData(catalogueKey, rows)
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Host />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(document.body.textContent).toContain('unavailable'))
    expect(button('Run new test')).toBeUndefined()
    expect(posts()).toHaveLength(0)
    expect(document.body.textContent).not.toContain('First · crd_first')
  },
)
it.each(['session', 'permission', 'credential', 'expiry'])(
  'fences a captured locked-row dispatch after same-turn %s change',
  async (boundary) => {
    lockedCredential = 'crd_selected'
    await mountLocked()
    const invoke = captureRun()
    await act(async () => {
      if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else if (boundary === 'permission') cache.setQueryData(permissionKey, ['providers.read'])
      else if (boundary === 'credential') {
        const rows = providers()
        rows[0].connections[0].credentials = [rows[0].connections[0].credentials[0]]
        cache.setQueryData(catalogueKey, rows)
      } else cache.setQueryData(sessionKey, { user: { id: 'usr_one' }, csrf_token: 'renewed' })
      invoke()
    })
    expect(posts()).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Explicit pending')
  },
)
it('aborts an unkeyed row Credential retarget and rejects its held result', async () => {
  lockedCredential = 'crd_selected'
  hold = new Promise((resolve) => {
    release = resolve
  })
  await mountLocked()
  await click('Run new test')
  await until(() => expect(signal).toBeTruthy())
  await act(async () => {
    lockedCredential = 'crd_first'
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    )
  })
  expect(signal?.aborted).toBe(true)
  await act(async () => release?.())
  expect(posts()).toHaveLength(1)
  expect(document.body.textContent).not.toContain('This test passed.')
  expect(document.body.textContent).not.toContain('First · crd_first')
})
it('keeps a locked-row unknown result as a new explicit observation of the same Credential', async () => {
  lockedCredential = 'crd_selected'
  testStatus = 503
  await mountLocked()
  await click('Run new test')
  await until(() => expect(document.body.textContent).toContain('previous result is unknown'))
  expect(posts()).toHaveLength(1)
  expect(button('Credential to test')).toBeUndefined()
  testStatus = 200
  await click('Run new test')
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(posts().map((request) => JSON.parse(request.data))).toEqual([
    { credential_id: 'crd_selected' },
    { credential_id: 'crd_selected' },
  ])
})
