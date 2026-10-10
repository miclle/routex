import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import RegistrationPage from './registration'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const old = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  policy: {
    enabled: boolean
    approval_required: boolean
    allowed_email_domains: string[]
    review_etag: string
  },
  role: 'admin' | 'member',
  status: number,
  commit: boolean,
  permissionStatus: number,
  csrf: string,
  writePause: Promise<void> | undefined
const tick = () => new Promise((r) => setTimeout(r, 0))
async function flush() {
  for (let n = 0; n < 8; n++)
    await act(async () => {
      await tick()
    })
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/admin/auth']}>
          <RegistrationPage />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await flush()
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await flush()
}
async function reason(value = 'Complete reviewed policy') {
  const input = document.querySelector<HTMLTextAreaElement>('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await flush()
}
async function save() {
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await flush()
}
const writes = () => requests.filter((r) => r.method === 'patch')
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  requests = []
  policy = {
    enabled: true,
    approval_required: true,
    allowed_email_domains: [],
    review_etag: 'a'.repeat(64),
  }
  role = 'admin'
  status = 0
  commit = false
  permissionStatus = 0
  csrf = 'current'
  writePause = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session')
      data = {
        user: { id: 'usr_admin', email: 'admin@example.invalid', name: 'Actor', role },
        csrf_token: csrf,
      }
    else if (config.url === '/auth/permissions') {
      if (permissionStatus)
        throw new AxiosError('Controlled permission failure', '', config, undefined, {
          config,
          status: permissionStatus,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      data = { permissions: ['registration.write'] }
    } else if (config.url === '/admin/auth/oidc' && config.method === 'get')
      return {
        config,
        status: 503,
        statusText: '',
        headers: new AxiosHeaders(),
        data: {},
      }
    else if (config.url === '/admin/registration' && config.method === 'get') data = { ...policy }
    else if (config.url === '/admin/registration' && config.method === 'patch') {
      const failure = status
      await writePause
      const input = JSON.parse(config.data)
      if (!failure || commit)
        policy = {
          enabled: input.enabled,
          approval_required: input.approval_required,
          allowed_email_domains: [...input.allowed_email_domains],
          review_etag: 'b'.repeat(64),
        }
      if (failure)
        throw new AxiosError('Controlled error', '', config, undefined, {
          config,
          status: failure,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      data = { ...policy, confirmation: 'current_registration_policy' }
    } else if (config.url === '/auth/registration')
      data = {
        enabled: policy.enabled,
        approval_required: policy.approval_required,
        allowed_email_domains: policy.enabled ? [...policy.allowed_email_domains] : [],
      }
    else throw new Error('Unexpected ' + config.url)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ ETag: `"${policy.review_etag}"` }),
      data,
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = old
})
it('requires intrinsic administrator even when a non-admin has registration.write', async () => {
  role = 'member'
  await mount()
  expect(requests.some((r) => r.url === '/admin/registration')).toBe(false)
  expect(document.querySelector('[role="switch"]')).toBeNull()
  expect(writes()).toHaveLength(0)
})
it('preserves approval_required when closing registration and confirms complete reviewed policy', async () => {
  await mount()
  await click('Configure')
  const switches = document.querySelectorAll<HTMLButtonElement>('[role="switch"]')
  expect(switches).toHaveLength(2)
  await act(async () => switches[0].click())
  expect(switches[1].getAttribute('aria-checked')).toBe('true')
  await save()
  expect(writes()).toHaveLength(0)
  await reason()
  await save()
  expect(writes()).toHaveLength(0)
  expect(document.body.textContent).toContain('Confirm registration policy')
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  expect(JSON.parse(writes()[0].data)).toEqual({
    enabled: false,
    approval_required: true,
    allowed_email_domains: [],
    reason: 'Complete reviewed policy',
  })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  expect(policy.approval_required).toBe(true)
  expect(document.body.textContent).toContain('Current registration policy confirmed')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('retains an uncertain complete policy intent through drawer dismissal and current GET', async () => {
  status = 503
  commit = true
  await mount()
  await click('Configure')
  await reason('Exact policy intent')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await save()
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  expect(document.body.textContent).toContain('unconfirmed')
  await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'registration'] }))
  await flush()
  expect(document.body.textContent).not.toContain('Current registration policy confirmed')
  status = 409
  await click('Retry exact submitted request')
  status = 0
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(3)
  for (const request of writes()) {
    expect(JSON.parse(request.data)).toEqual({
      enabled: false,
      approval_required: true,
      allowed_email_domains: [],
      reason: 'Exact policy intent',
    })
    expect(request.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  }
})
it('hides policy and draft immediately during permission renewal without dropping the exact intent', async () => {
  await mount()
  await click('Configure')
  await reason('Retained policy draft')
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['permissions', 'usr_admin'], refetchType: 'none' }),
  )
  await flush()
  expect(document.querySelector('[role="switch"]')).toBeNull()
  expect(document.querySelector('textarea')).toBeNull()
  expect(writes()).toHaveLength(0)
})
it('keeps bilingual switch values and reason after live language change', async () => {
  await mount()
  await click('Configure')
  await reason('Retained bilingual draft')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelectorAll('[role="switch"]')[1].getAttribute('aria-label')).toBe(
    '注册需要审批',
  )
  expect(document.querySelectorAll('[role="switch"]')[1].getAttribute('aria-checked')).toBe('true')
  expect(document.querySelector('textarea')!.value).toBe('Retained bilingual draft')
  expect(writes()).toHaveLength(0)
})

it('does not retain a policy confirmation notice after a different current policy generation appears', async () => {
  await mount()
  await click('Configure')
  await reason()
  await save()
  await click('Confirm')
  expect(document.body.textContent).toContain('Current registration policy confirmed')
  policy = {
    enabled: false,
    approval_required: false,
    allowed_email_domains: [],
    review_etag: 'c'.repeat(64),
  }
  await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'registration'] }))
  await flush()
  expect(document.body.textContent).not.toContain('Current registration policy confirmed')
})
it('returns confirmation cancellation focus to the exact connected Save button within its drawer', async () => {
  await mount()
  await click('Configure')
  await reason()
  const saveButton = button('Save registration settings')
  await act(async () => saveButton.focus())
  await save()
  await click('Cancel')
  expect(document.activeElement).toBe(saveButton)
  expect(writes()).toHaveLength(0)
})

it('retains the exact uncertain policy through failed permission refetch and fresh recovery', async () => {
  policy.allowed_email_domains = ['example.invalid']
  status = 503
  await mount()
  await click('Configure')
  await reason('Survive permission failure')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await save()
  await click('Confirm')
  const original = writes()[0]
  expect(original).toBeTruthy()
  expect(JSON.parse(original.data).allowed_email_domains).toEqual(['example.invalid'])
  permissionStatus = 503
  await act(async () => cache.invalidateQueries({ queryKey: ['permissions', 'usr_admin'] }))
  await flush()
  expect(cache.getQueryState(['permissions', 'usr_admin'])!.status).toBe('error')
  expect(document.querySelector('textarea')).toBeNull()
  expect(document.querySelector('[role="switch"]')).toBeNull()
  expect(button('Retry exact submitted request')).toBeUndefined()
  expect(document.body.textContent).not.toContain('example.invalid')
  expect(writes()).toHaveLength(1)
  permissionStatus = 0
  await act(async () => cache.refetchQueries({ queryKey: ['permissions', 'usr_admin'] }))
  await flush()
  expect(document.querySelector('textarea')!.value).toBe('Survive permission failure')
  expect(document.body.textContent).toContain('example.invalid')
  expect(writes()).toHaveLength(1)
  status = 409
  await click('Retry exact submitted request')
  status = 0
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(3)
  expect(
    writes().every(
      (r) =>
        r.data === original.data && r.headers.get('If-Match') === original.headers.get('If-Match'),
    ),
  ).toBe(true)
})

it.each([409, 412])(
  'retains first policy %s and permits a new review only after explicit abandonment',
  async (failure) => {
    status = failure
    await mount()
    await click('Configure')
    await reason('Original reviewed policy')
    await save()
    await click('Confirm')
    const original = writes()[0]
    expect(button('Retry exact submitted request')).toBeTruthy()
    expect(button('Save registration settings')).toBeUndefined()
    await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'registration'] }))
    await flush()
    csrf = 'renewed-policy'
    await act(async () => cache.invalidateQueries({ queryKey: ['auth', 'session'] }))
    await flush()
    expect(writes()).toHaveLength(1)
    status = 503
    await click('Retry exact submitted request')
    expect(writes()).toHaveLength(2)
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('renewed-policy')
    await click('Abandon submitted request')
    expect(document.body.textContent).toContain('previous outcome remains unknown')
    expect(document.body.textContent).not.toContain('Current registration policy confirmed')
    expect(document.querySelector('textarea')!.value).toBe('Original reviewed policy')
    await save()
    expect(button('Confirm')).toBeUndefined()
    await click('Review current state')
    status = 0
    await save()
    await click('Confirm')
    expect(writes()).toHaveLength(3)
  },
)
it('localizes explicit policy abandonment without changing the original body or claiming rollback', async () => {
  status = 412
  await mount()
  await click('Configure')
  await reason('Bilingual unknown policy')
  await save()
  await click('Confirm')
  const original = writes()[0]
  await act(async () => i18n.changeLanguage('zh'))
  expect(button('放弃已提交请求')).toBeTruthy()
  expect(document.body.textContent).toContain('原请求的结果仍未知')
  await click('放弃已提交请求')
  expect(document.querySelector('textarea')!.value).toBe('Bilingual unknown policy')
  expect(document.body.textContent).toContain('原请求的结果仍未知')
  expect(writes()).toHaveLength(1)
  expect(JSON.parse(original.data).reason).toBe('Bilingual unknown policy')
})

it('does not abandon a pending policy or accept its late result after same-actor renewed authority', async () => {
  let release!: () => void
  writePause = new Promise<void>((resolve) => {
    release = resolve
  })
  await mount()
  await click('Configure')
  await reason('Late policy outcome')
  await save()
  await click('Confirm')
  const original = writes()[0]
  expect(button('Abandon submitted request').disabled).toBe(true)
  await click('Abandon submitted request')
  expect(writes()).toHaveLength(1)
  permissionStatus = 503
  await act(async () => cache.invalidateQueries({ queryKey: ['permissions', 'usr_admin'] }))
  await flush()
  expect(document.querySelector('textarea')).toBeNull()
  release()
  await flush()
  permissionStatus = 0
  await act(async () => cache.refetchQueries({ queryKey: ['permissions', 'usr_admin'] }))
  await flush()
  expect(button('Retry exact submitted request')).toBeTruthy()
  expect(document.body.textContent).not.toContain('Current registration policy confirmed')
  expect(writes()).toHaveLength(1)
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(2)
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
})

it.each([409, 412])(
  'does not resolve simulated committed policy %s from a matching GET or rejected retry',
  async (failure) => {
    status = failure
    commit = true
    await mount()
    await click('Configure')
    await reason('Original committed policy')
    await save()
    await click('Confirm')
    const original = writes()[0]
    expect(policy.review_etag).toBe('b'.repeat(64))
    await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'registration'] }))
    await flush()
    expect(button('Retry exact submitted request')).toBeTruthy()
    expect(document.body.textContent).not.toContain('Current registration policy confirmed')
    status = 412
    await click('Retry exact submitted request')
    expect(button('Abandon submitted request')).toBeTruthy()
    expect(writes()).toHaveLength(2)
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    status = 0
    await click('Retry exact submitted request')
    expect(writes()).toHaveLength(3)
    expect(writes()[2].data).toBe(original.data)
    expect(writes()[2].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(document.body.textContent).toContain('Current registration policy confirmed')
  },
)

async function domain(value: string) {
  const node = document.querySelector<HTMLInputElement>('input[aria-label="Email domain"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await flush()
}
it('adds canonical exact domain chips by keyboard, rejects duplicates and confirms a complete replacement', async () => {
  await mount()
  await click('Configure')
  await domain(' Sub.Example.INVALID ')
  const keyboard = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
  await act(async () =>
    document
      .querySelector<HTMLInputElement>('input[aria-label="Email domain"]')!
      .dispatchEvent(keyboard),
  )
  expect(keyboard.defaultPrevented).toBe(true)
  expect(writes()).toHaveLength(0)
  expect(document.querySelector('[aria-label="Remove sub.example.invalid"]')).not.toBeNull()
  await domain('SUB.EXAMPLE.INVALID')
  await click('Add')
  expect(document.body.textContent).toContain('already listed')
  await domain('example.invalid')
  await click('Add')
  await reason('Reviewed exact domain list')
  await save()
  expect(document.body.textContent).toContain(
    'Allowed email domains: example.invalid, sub.example.invalid.',
  )
  await click('Confirm')
  expect(JSON.parse(writes()[0].data)).toEqual({
    enabled: true,
    approval_required: true,
    allowed_email_domains: ['example.invalid', 'sub.example.invalid'],
    reason: 'Reviewed exact domain list',
  })
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('retains original domain bytes and ETag through first409, dismissal, renewed policy and503 until exact success', async () => {
  status = 409
  commit = true
  await mount()
  await click('Configure')
  await domain('example.invalid')
  await click('Add')
  await reason('Immutable domain intent')
  await save()
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  expect(document.body.textContent).toContain('unconfirmed')
  const original = writes()[0]
  expect(
    document.querySelector<HTMLButtonElement>('[aria-label="Remove example.invalid"]')!.disabled,
  ).toBe(true)
  await act(async () =>
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await flush()
  expect(document.querySelector('textarea')).toBeNull()
  await click('Configure')
  expect(document.querySelector('textarea')!.value).toBe('Immutable domain intent')
  status = 503
  await click('Retry exact submitted request')
  status = 0
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(3)
  for (const request of writes()) {
    expect(request.data).toBe(original.data)
    expect(request.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  }
  expect(JSON.parse(original.data).allowed_email_domains).toEqual(['example.invalid'])
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('keeps reviewed chips and safe draft through language switch and distinguishes explicit unrestricted replacement', async () => {
  policy.allowed_email_domains = ['example.invalid']
  await mount()
  await click('Configure')
  await reason('Explicit unrestricted replacement')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('[aria-label="移除 example.invalid"]')).not.toBeNull()
  expect(document.querySelector('textarea')!.value).toBe('Explicit unrestricted replacement')
  await act(async () =>
    document.querySelector<HTMLButtonElement>('[aria-label="移除 example.invalid"]')!.click(),
  )
  await act(async () => i18n.changeLanguage('en'))
  await save()
  expect(document.body.textContent).toContain('Allowed email domains: All domains.')
  await click('Confirm')
  expect(JSON.parse(writes()[0].data).allowed_email_domains).toEqual([])
})
