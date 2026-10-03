import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ProjectCreationContext } from '@/types/resources'
import {
  createProjectResources,
  getProjectCreationContext,
  getProjectCreationModels,
  validProjectCreationReason,
} from '@/api/resources'
import i18n from '@/i18n'
import CreateResourcePage from './create'
import { creationResources, emptyCreationResources } from './project-creation-values'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const stamp = '2026-10-04T02:00:00Z'
const manager = { id: 'usr_one', name: 'Current creator', email: 'creator@example.invalid' }
const other = { id: 'usr_two', name: 'Second manager', email: 'second@example.invalid' }
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  context: ProjectCreationContext,
  requests: InternalAxiosRequestConfig[],
  permissions: string[]
let failures: Record<string, number>,
  application: 'applied' | 'pending' | 'superseded' | 'unavailable',
  modelReply: unknown
let holds: Record<string, (() => Promise<void>) | undefined>
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  permissions = []
  failures = {}
  holds = {}
  application = 'applied'
  context = {
    review_etag: 'a'.repeat(64),
    platform_currency: 'USD',
    can_set_models: true,
    can_set_limits: true,
    can_request_resources: true,
  }
  modelReply = { items: [{ id: 'mdl_one', name: 'First model' }], next_cursor: null }
  identity = { user: { ...manager, role: 'member' }, csrf_token: 'current-csrf' }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    await holds[config.url!]?.()
    const response = {
      config,
      status: failures[config.url!] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = identity
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/project-creation-resources') response.data = context
    else if (config.url === '/project-creation-models') response.data = modelReply
    else if (config.url === '/projects/creation-manager-candidates')
      response.data = { items: config.params.q ? [other] : [manager, other] }
    else if (config.url === '/projects' && config.method === 'post') {
      const body = JSON.parse(config.data),
        initial = body.initial_request
      const count = initial
        ? Number(!!initial.model_ids?.length) +
          Number(initial.tokens_month !== undefined || initial.money_month !== undefined) +
          Number(
            initial.rpm !== undefined ||
              initial.tpm !== undefined ||
              initial.concurrency !== undefined,
          )
        : 0
      response.data = {
        committed: true,
        receipt: {
          creation_id: body.creation_id,
          project_id: 'prj_one',
          created_at: stamp,
          initial_request_ids: Array.from({ length: count }, (_, index) => `pmr_${index}`),
        },
        runtime_applied: application === 'applied',
        application_status: application,
        project:
          application === 'unavailable'
            ? null
            : {
                id: 'prj_one',
                name: body.name,
                description: body.description,
                status: 'active',
                created_at: stamp,
                model_ids: body.initial_resources?.model_ids ?? [],
                managers: body.manager_ids.map((id: string) => ({
                  id: `pmg_${id}`,
                  user_id: id,
                  name: manager.name,
                  email: manager.email,
                })),
              },
      }
    }
    if (response.status >= 400)
      throw new AxiosError('Current authority denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assertion: () => void) {
  for (let i = 0; i < 120; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 119) throw error
    }
  }
}
async function mount() {
  router = createMemoryRouter(
    [
      { path: '/projects/new', element: <CreateResourcePage kind="projects" /> },
      { path: '/projects/:id', element: <p>Project</p> },
    ],
    { initialEntries: ['/projects/new'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('fieldset')?.disabled).toBe(false))
  await fill('[name="name"]', 'Created Project')
}
async function fill(selector: string, value: string) {
  const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function clickText(text: string) {
  const element = [...host.querySelectorAll('button')].find((item) => item.textContent === text)!
  expect(element).toBeTruthy()
  await act(async () => element.click())
}
async function click(selector: string) {
  await act(async () => host.querySelector<HTMLElement>(selector)!.click())
}
async function submit() {
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
function posts() {
  return requests.filter((item) => item.method === 'post')
}
async function refresh() {
  await clickText('Refresh creation context')
  await until(() => expect(host.querySelector('fieldset')?.disabled).toBe(false))
}

describe('Project initial resources and immutable creation', () => {
  it('omits blank policies and retains exact zero/decimal strings in the reviewed direct intent', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('First model'))
    await click('section[aria-label="Initial resources"] input[type="checkbox"]')
    await fill('[aria-label="Monthly Tokens"]', '0')
    await fill('[aria-label="Monthly money (USD)"]', '0.000000000000000001')
    await fill('[aria-label="RPM"]', '10')
    await fill('[aria-label="Resource reason"]', 'Initial allocation')
    await submit()
    await until(() => expect(host.textContent).toContain('The creation was committed.'))
    const request = posts()[0],
      body = JSON.parse(request.data)
    expect(body.initial_resources).toEqual({
      model_ids: ['mdl_one'],
      tokens_month: 0,
      money_month: '0.000000000000000001',
      currency: 'USD',
      rpm: 10,
      reason: 'Initial allocation',
    })
    expect(body.initial_request).toBeUndefined()
    expect(body.creation_id).toMatch(/^[a-f0-9-]{36}$/)
    expect(request.headers.get('If-Match')).toBe(`"${context.review_etag}"`)
    expect(request.headers.get('X-CSRF-Token')).toBe('current-csrf')
    expect(router.state.location.pathname).toBe('/projects/new')
    expect(requests.some((item) => /admin\/models|admin\/members|\/keys/.test(item.url!))).toBe(
      false,
    )
  })
  it.each(['models', 'limits'] as const)(
    'keeps %s direct authority independent of role and metadata write',
    async (kind) => {
      identity.user.role = 'admin'
      permissions = ['projects.write']
      context.can_set_models = kind === 'models'
      context.can_set_limits = kind === 'limits'
      context.can_request_resources = false
      await mount()
      expect(!!host.querySelector('[aria-label="Monthly Tokens"]')).toBe(kind === 'limits')
      expect(!!host.querySelector('[aria-label="Search initial models"]')).toBe(kind === 'models')
      await submit()
      await until(() => expect(posts()).toHaveLength(1))
      expect(JSON.parse(posts()[0].data).initial_resources).toBeUndefined()
    },
  )
  it('uses one request object and explicitly selected applicant for all three pending request groups', async () => {
    context.can_set_models = false
    context.can_set_limits = false
    await mount()
    await act(async () =>
      [...host.querySelectorAll('label')]
        .find((label) => label.textContent === 'Request initial resources for approval')!
        .querySelector<HTMLInputElement>('input')!
        .click(),
    )
    await until(() => expect(host.textContent).toContain('First model'))
    await click('section[aria-label="Initial resource request"] input[type="checkbox"]')
    await fill('[aria-label="Monthly Tokens"]', '500')
    await fill('[aria-label="Concurrency"]', '2')
    await fill('[aria-label="Resource reason"]', 'Need initial access')
    application = 'pending'
    await submit()
    await until(() => expect(host.textContent).toContain('3 initial requests recorded.'))
    const body = JSON.parse(posts()[0].data)
    expect(body.initial_resources).toBeUndefined()
    expect(body.initial_request).toEqual({
      model_ids: ['mdl_one'],
      tokens_month: 500,
      concurrency: 2,
      reason: 'Need initial access',
    })
    expect(body.manager_ids).toContain(manager.id)
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('已记录 3 项初始申请')
    expect(host.querySelector<HTMLInputElement>('[name="name"]')!.value).toBe('Created Project')
  })
  it('requires explicit currency review and preserves decimal drafts across a newer context', async () => {
    await mount()
    await fill('[aria-label="Monthly money (USD)"]', '12.000000000000000001')
    await fill('[aria-label="Resource reason"]', 'Reviewed allocation')
    context = { ...context, review_etag: 'b'.repeat(64), platform_currency: 'CNY' }
    await refresh()
    expect(host.textContent).toContain('Creation authority or currency changed.')
    await submit()
    expect(posts()).toHaveLength(0)
    await clickText('Review current creation context')
    await until(() =>
      expect(
        host.querySelector<HTMLInputElement>('[aria-label="Monthly money (CNY)"]')?.value,
      ).toBe('12.000000000000000001'),
    )
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).initial_resources.currency).toBe('CNY')
    expect(posts()[0].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
  })
  it('retains exact UUID/body/ETag through unknown, 403 and 409 retries using refreshed same-actor CSRF', async () => {
    await mount()
    failures['/projects'] = 503
    await submit()
    await until(() => expect(host.textContent).toContain('Creation may already be saved.'))
    const original = posts()[0]
    await submit()
    expect(posts()).toHaveLength(1)
    for (const status of [403, 409]) {
      failures['/projects'] = status
      await clickText('Retry original creation')
      await until(() => expect(posts().at(-1)?.data).toBe(original.data))
      await until(() =>
        expect(
          [...host.querySelectorAll('button')].find(
            (button) => button.textContent === 'Retry original creation',
          )?.disabled,
        ).toBe(false),
      )
      expect(host.textContent).not.toContain('Review current creation context')
    }
    identity = { ...identity, csrf_token: 'rotated-csrf' }
    await act(async () => cache.setQueryData(sessionKey, identity))
    context = {
      ...context,
      review_etag: 'c'.repeat(64),
      can_set_models: false,
      can_set_limits: false,
    }
    delete failures['/projects']
    application = 'unavailable'
    await until(() =>
      expect(
        [...host.querySelectorAll('button')].find(
          (button) => button.textContent === 'Retry original creation',
        )?.disabled,
      ).toBe(false),
    )
    await clickText('Retry original creation')
    await until(() =>
      expect(host.textContent).toContain('Current Project access and application are unavailable.'),
    )
    expect(posts()).toHaveLength(4)
    for (const request of posts()) {
      expect(request.data).toBe(original.data)
      expect(request.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    }
    expect(posts().at(-1)?.headers.get('X-CSRF-Token')).toBe('rotated-csrf')
    expect(host.querySelector('a[href="/projects/prj_one"]')).toBeNull()
    await submit()
    expect(posts()).toHaveLength(4)
  })
  it('preserves historical receipt while rechecking current superseded application without stale claims', async () => {
    await mount()
    await submit()
    await until(() =>
      expect(host.textContent).toContain('The recorded creation is currently applied.'),
    )
    const original = posts()[0]
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/projects'] = () => wait
    application = 'superseded'
    await clickText('Recheck recorded creation')
    expect(host.textContent).toContain('Created Project: prj_one')
    expect(host.textContent).not.toContain('The recorded creation is currently applied.')
    expect(host.querySelector('a[href="/projects/prj_one"]')).toBeNull()
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('superseded by current Project changes'))
    expect(posts()[1].data).toBe(original.data)
  })
  it('hides private manager/model selections and blocks dispatch during renewed actor reads', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('First model'))
    await click('section[aria-label="Initial resources"] input[type="checkbox"]')
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/auth/session'] = () => wait
    let refreshed!: Promise<unknown>
    await act(async () => {
      refreshed = cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).not.toContain('First model'))
    expect(
      host.querySelector('[aria-label="Selected Project managers"]')?.hasAttribute('hidden'),
    ).toBe(true)
    await submit()
    expect(posts()).toHaveLength(0)
    await act(async () => release())
    await act(async () => refreshed)
    await until(() => expect(host.textContent).toContain('First model'))
    expect(
      requests.filter((item) => item.url === '/project-creation-resources').length,
    ).toBeGreaterThan(1)
  })
  it('retains selected models outside a bounded search and blocks after candidate authority fails', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('First model'))
    await click('section[aria-label="Initial resources"] input[type="checkbox"]')
    modelReply = { items: [], next_cursor: null }
    await fill('[aria-label="Search initial models"]', 'literal%_')
    await until(() =>
      expect(host.querySelector('[aria-label="Selected initial models"]')?.textContent).toContain(
        'First model',
      ),
    )
    await fill('[aria-label="Resource reason"]', 'Need model')
    failures['/project-creation-models'] = 403
    await act(async () => cache.refetchQueries({ queryKey: ['project-creation-models'] }))
    await until(() => expect(host.textContent).not.toContain('First model'))
    await submit()
    expect(posts()).toHaveLength(0)
    expect(requests.find((item) => item.params?.q === 'literal%_')?.params.limit).toBe(50)
  })
  it('rejects request mode after an administrator removes the applicant from initial managers', async () => {
    permissions = ['projects.write']
    await mount()
    await click('[aria-label="Remove manager Current creator"]')
    await act(async () =>
      [...host.querySelectorAll('label')]
        .find((label) => label.textContent === 'Second managersecond@example.invalid')
        ?.querySelector<HTMLInputElement>('input')
        ?.click(),
    )
    await act(async () =>
      [...host.querySelectorAll('label')]
        .find((label) => label.textContent === 'Request initial resources for approval')!
        .querySelector<HTMLInputElement>('input')!
        .click(),
    )
    await submit()
    expect(posts()).toHaveLength(0)
    expect(host.textContent).toContain('Select yourself as an initial Project manager')
  })

  it('retains a committed historical receipt but clears current facts after renewal or failed recheck', async () => {
    await mount()
    await submit()
    await until(() =>
      expect(host.textContent).toContain('The recorded creation is currently applied.'),
    )
    const original = posts()[0]
    identity = { ...identity, csrf_token: 'renewed-csrf' }
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() =>
      expect(host.textContent).toContain(
        'Recheck the recorded creation to confirm current Project access and application.',
      ),
    )
    expect(host.textContent).toContain('Created Project: prj_one')
    expect(host.textContent).not.toContain('The recorded creation is currently applied.')
    expect(posts()).toHaveLength(1)
    failures['/projects'] = 403
    await clickText('Recheck recorded creation')
    await until(() =>
      expect(host.textContent).toContain('Current creation application could not be confirmed.'),
    )
    expect(host.textContent).toContain('Created Project: prj_one')
    expect(host.textContent).not.toContain('Creation was rejected.')
    expect(host.querySelector('a[href="/projects/prj_one"]')).toBeNull()
    expect(posts()[1].data).toBe(original.data)
  })
  it('keeps an in-flight acknowledgement through renewed reads without replay or stale application', async () => {
    await mount()
    let releasePost!: () => void, releaseSession!: () => void
    const postWait = new Promise<void>((resolve) => {
      releasePost = resolve
    })
    holds['/projects'] = () => postWait
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    await submit()
    expect(posts()).toHaveLength(1)
    const sessionWait = new Promise<void>((resolve) => {
      releaseSession = resolve
    })
    holds['/auth/session'] = () => sessionWait
    let renewed!: Promise<unknown>
    await act(async () => {
      renewed = cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.querySelector('fieldset')?.disabled).toBe(true))
    await act(async () => releasePost())
    await until(() => expect(host.textContent).toContain('The creation was committed.'))
    expect(host.querySelector('[aria-label="Recorded Project creation"]')).toBeNull()
    await act(async () => releaseSession())
    await act(async () => renewed)
    await until(() => expect(host.textContent).toContain('Created Project: prj_one'))
    expect(host.textContent).toContain(
      'Recheck the recorded creation to confirm current Project access and application.',
    )
    expect(posts()).toHaveLength(1)
    expect(
      [...host.querySelectorAll('button')].find(
        (button) => button.textContent === 'Recheck recorded creation',
      )?.disabled,
    ).toBe(false)
  })
  it('requires explicit clearing of preserved direct drafts when their authority is revoked', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('First model'))
    await click('section[aria-label="Initial resources"] input[type="checkbox"]')
    await fill('[aria-label="Monthly Tokens"]', '50')
    await fill('[aria-label="Resource reason"]', 'Initial allocation')
    context = {
      ...context,
      review_etag: 'd'.repeat(64),
      can_set_models: false,
      can_set_limits: false,
    }
    await refresh()
    expect(host.textContent).not.toContain('First model')
    await submit()
    expect(posts()).toHaveLength(0)
    await clickText('Review current creation context')
    await submit()
    expect(posts()).toHaveLength(0)
    await clickText('Clear initial models requiring unavailable permission')
    await clickText('Clear initial limits requiring unavailable permission')
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).initial_resources).toBeUndefined()
  })
  it('retains unknown intent through failed current context reads and retries only its original body', async () => {
    await mount()
    failures['/projects'] = 503
    await submit()
    await until(() => expect(host.textContent).toContain('Creation may already be saved.'))
    const original = posts()[0]
    failures['/project-creation-resources'] = 403
    await clickText('Refresh creation context')
    await until(() =>
      expect(host.textContent).toContain('You do not have permission for this action'),
    )
    await submit()
    expect(posts()).toHaveLength(1)
    delete failures['/projects']
    application = 'unavailable'
    await clickText('Retry original creation')
    await until(() => expect(host.textContent).toContain('The creation was committed.'))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  })
})
describe('Project creation API and flat resource values', () => {
  it('omits absent search and cursor parameters accepted by the strict picker endpoint', async () => {
    await getProjectCreationModels('', null)
    const empty = new URL(client.getUri(requests[0]), 'http://routex.test')
    expect(empty.searchParams.has('q')).toBe(false)
    expect(empty.searchParams.has('cursor')).toBe(false)
    expect(empty.searchParams.get('limit')).toBe('50')
    await getProjectCreationModels('literal%_', 'mdl_next')
    const searched = new URL(client.getUri(requests[1]), 'http://routex.test')
    expect(searched.searchParams.get('q')).toBe('literal%_')
    expect(searched.searchParams.get('cursor')).toBe('mdl_next')
  })

  it('preserves zero, omits blanks and rejects unsafe integers/exponent money', () => {
    expect(creationResources(emptyCreationResources(), 'USD')).toBeNull()
    expect(
      creationResources(
        { ...emptyCreationResources(), tokens_month: '0', money_month: '0.000000000000000001' },
        'USD',
      ),
    ).toEqual({ tokens_month: 0, money_month: '0.000000000000000001', currency: 'USD', reason: '' })
    expect(
      creationResources({ ...emptyCreationResources(), rpm: '9007199254740992' }, 'USD'),
    ).toBeUndefined()
    expect(
      creationResources({ ...emptyCreationResources(), money_month: '1e6' }, 'USD'),
    ).toBeUndefined()
  })
  it('counts raw UTF-8 reason bytes and rejects control characters', () => {
    expect(validProjectCreationReason('界'.repeat(341) + 'x')).toBe(true)
    expect(validProjectCreationReason('界'.repeat(342))).toBe(false)
    expect(validProjectCreationReason('reason\nnext')).toBe(false)
    expect(validProjectCreationReason('   ')).toBe(false)
  })
  it('validates context, bounded unique candidates and exact creation receipts', async () => {
    context.platform_currency = 'usd'
    await expect(getProjectCreationContext()).rejects.toThrow()
    modelReply = {
      items: [
        { id: 'mdl_one', name: 'a' },
        { id: 'mdl_one', name: 'b' },
      ],
      next_cursor: null,
    }
    await expect(getProjectCreationModels('', null)).rejects.toThrow()
    await expect(
      createProjectResources(
        {
          body: { creation_id: 'bad', name: 'Project', description: '', manager_ids: [manager.id] },
          etag: 'a'.repeat(64),
        },
        'csrf',
      ),
    ).rejects.toThrow()
    expect(posts()).toHaveLength(0)
  })

  it.each(['uuid', 'project', 'children', 'commit', 'runtime'] as const)(
    'rejects a malformed %s receipt without accepting current facts',
    async (variant) => {
      const adapter = client.defaults.adapter as AxiosAdapter
      client.defaults.adapter = async (config) => {
        const response = await adapter(config)
        if (config.method === 'post') {
          const value = response.data
          if (variant === 'uuid') value.receipt.creation_id = crypto.randomUUID()
          if (variant === 'project') value.project.id = 'prj_wrong'
          if (variant === 'children') value.receipt.initial_request_ids = ['pmr_wrong']
          if (variant === 'commit') value.committed = false
          if (variant === 'runtime') value.runtime_applied = false
        }
        return response
      }
      await expect(
        createProjectResources(
          {
            body: {
              creation_id: crypto.randomUUID(),
              name: 'Project',
              description: '',
              manager_ids: [manager.id],
            },
            etag: 'a'.repeat(64),
          },
          'csrf',
        ),
      ).rejects.toThrow('Unconfirmed')
      expect(posts()).toHaveLength(1)
    },
  )
  it.each(['mixed', 'empty', 'null', 'unsafe', 'currency', 'control'] as const)(
    'rejects %s resource input before dispatch',
    async (variant) => {
      const resources = { tokens_month: 0, reason: 'Allocation' }
      const body = {
        creation_id: crypto.randomUUID(),
        name: 'Project',
        description: '',
        manager_ids: [manager.id],
        initial_resources: resources,
      } as unknown as import('@/types/resources').ProjectResourceCreationInput
      if (variant === 'mixed') body.initial_request = resources
      if (variant === 'empty') body.initial_resources = { reason: 'Allocation' }
      if (variant === 'null')
        body.initial_resources = {
          tokens_month: null,
          reason: 'Allocation',
        } as unknown as typeof resources
      if (variant === 'unsafe')
        body.initial_resources = { tokens_month: Number.MAX_SAFE_INTEGER + 1, reason: 'Allocation' }
      if (variant === 'currency') body.initial_resources = { ...resources, currency: 'USD' }
      if (variant === 'control')
        body.initial_resources = { ...resources, reason: 'Allocation\nreason' }
      await expect(createProjectResources({ body, etag: 'a'.repeat(64) }, 'csrf')).rejects.toThrow(
        'Invalid Project creation intent',
      )
      expect(posts()).toHaveLength(0)
    },
  )
  it('rejects overflowing and repeating-cursor model pages', async () => {
    modelReply = {
      items: Array.from({ length: 51 }, (_, index) => ({ id: `mdl_${index}`, name: 'Model' })),
      next_cursor: null,
    }
    await expect(getProjectCreationModels('', null)).rejects.toThrow()
    modelReply = { items: [], next_cursor: 'repeat' }
    await expect(getProjectCreationModels('', 'repeat')).rejects.toThrow()
  })
})
