import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/projectKeys'
import zh from '@/i18n/locales/zh/projectKeys'
import type { ResourceRecord } from '@/types/resources'
import type { ProjectKey } from '@/types/project-keys'
import ProjectKeysPanel from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let project: ResourceRecord
let keys: ProjectKey[]
let requests: InternalAxiosRequestConfig[]
let failures: Record<string, number>
let permissions: string[]
let paginated: boolean
const originalAdapter = client.defaults.adapter
const bearer = 'rxp_once_fixture_secret'
const base = '/projects/prj_one/keys'
const fixtureKey = (id = 'pky_old', status: ProjectKey['status'] = 'active'): ProjectKey => ({
  id,
  project_id: 'prj_one',
  creator_id: 'usr_departed',
  name: id === 'pky_old' ? 'Production' : 'Replacement',
  prefix: 'rxp_masked',
  status,
  model_ids: ['mdl_project'],
  expires_at: null,
  created_at: '2026-09-23T00:00:00Z',
  replaces_key_id: null,
  delivery_expires_at: new Date(Date.now() + 600_000).toISOString(),
  delivery_mode: 'manual',
})
beforeEach(() => {
  i18n.addResourceBundle('en', 'projectKeys', en, true, true)
  i18n.addResourceBundle('zh', 'projectKeys', zh, true, true)
  project = {
    id: 'prj_one',
    name: 'Project',
    description: '',
    status: 'active',
    created_at: '',
    model_ids: ['mdl_project'],
    managers: [
      { id: 'pmg_one', user_id: 'usr_manager', name: 'Manager', email: 'manager@example.test' },
    ],
  }
  keys = []
  requests = []
  failures = {}
  permissions = []
  paginated = false
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const route = `${config.method} ${config.url}`
    const response = {
      config,
      status: failures[route] ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400) throw new AxiosError('Failure', '', config, undefined, response)
    if (route === 'get /auth/session')
      response.data = {
        user: { id: 'usr_manager', role: 'member', name: 'Manager', email: 'manager@example.test' },
        csrf_token: 'csrf',
      }
    if (route === 'get /auth/permissions') response.data = { permissions }
    if (route === `get ${base}`) {
      const filtered = keys.filter(
        (key) => !config.params?.status || key.status === config.params.status,
      )
      response.data = {
        items: structuredClone(
          paginated ? (config.params?.cursor ? filtered.slice(1) : filtered.slice(0, 1)) : filtered,
        ),
        next_cursor:
          paginated && !config.params?.cursor && filtered.length > 1 ? 'cursor_next' : null,
      }
    }
    if (route === `post ${base}`) {
      const key = { ...fixtureKey('pky_new', 'pending'), ...JSON.parse(config.data) }
      keys.unshift(key)
      response.data = { key: structuredClone(key), secret: bearer }
    }
    for (const key of [...keys]) {
      const path = `${base}/${key.id}`
      if (route === `post ${path}/rotate`) {
        const replacement = {
          ...key,
          id: 'pky_replacement',
          status: 'pending' as const,
          replaces_key_id: key.id,
          delivery_expires_at: new Date(Date.now() + 600_000).toISOString(),
        }
        keys.unshift(replacement)
        response.data = { key: structuredClone(replacement), secret: bearer }
      }
      if (route === `post ${path}/confirm`) {
        key.status =
          key.replaces_key_id &&
          keys.find((source) => source.id === key.replaces_key_id)?.status === 'disabled'
            ? 'disabled'
            : 'active'
        response.data = structuredClone(key)
      }
      if (route === `delete ${path}`) key.status = 'revoked'
      if (route === `patch ${path}`) {
        const data = JSON.parse(config.data)
        if (data.name) key.name = data.name
        if (typeof data.enabled === 'boolean') key.status = data.enabled ? 'active' : 'disabled'
        response.data = structuredClone(key)
      }
      if (route === `post ${path}/complete-rotation`) key.status = 'revoked'
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
})
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ProjectKeysPanel project={project} />
      </QueryClientProvider>,
    ),
  )
}
async function until(assert: () => void) {
  for (let index = 0; index < 60; index++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (index === 59) throw error
    }
  }
}
function button(text: string, scope: ParentNode = document) {
  const result = [...scope.querySelectorAll('button')].find((item) => item.textContent === text)
  expect(result, text).toBeDefined()
  return result!
}
async function click(text: string, scope?: ParentNode) {
  await act(async () => button(text, scope).click())
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
async function acknowledge() {
  await act(async () =>
    document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click(),
  )
}
async function createDelivery() {
  await render()
  await until(() => expect(button('Create project key').disabled).toBe(false))
  await click('Create project key')
  await fill('name', 'Application')
  await act(async () =>
    document.querySelector<HTMLInputElement>('input[name="model_ids"]')!.click(),
  )
  await submit()
  await until(() => expect(document.body.textContent).toContain(bearer))
}
function cachedData() {
  return JSON.stringify({
    queries: cache
      .getQueryCache()
      .getAll()
      .map((query) => query.state),
    mutations: cache
      .getMutationCache()
      .getAll()
      .map((mutation) => mutation.state),
  })
}

describe('Project key lifecycle', () => {
  it('gates metadata by current management authority, never creator identity or read-all permission', async () => {
    project.managers = []
    project.creator_id = 'usr_manager'
    permissions = ['projects.read_all']
    await render()
    await until(() => expect(container.textContent).toContain('Only current project managers'))
    expect(requests.some((request) => request.url === base)).toBe(false)
  })
  it('creates from project grants with CSRF and keeps one-time secrets out of every cache', async () => {
    await createDelivery()
    expect(cachedData()).not.toContain(bearer)
    expect(JSON.stringify(localStorage)).not.toContain(bearer)
    expect(JSON.stringify(sessionStorage)).not.toContain(bearer)
    expect(button('Confirm delivery').disabled).toBe(true)
    const create = requests.find((request) => request.method === 'post' && request.url === base)!
    expect(JSON.parse(create.data)).toEqual({
      name: 'Application',
      model_ids: ['mdl_project'],
      expires_at: null,
      delivery_mode: 'manual',
    })
    expect(create.headers.get('X-CSRF-Token')).toBe('csrf')
    expect(
      requests.some((request) => request.url === '/models' || request.url?.startsWith('/admin/')),
    ).toBe(false)
    await acknowledge()
    await click('Confirm delivery')
    await until(() => expect(document.body.textContent).not.toContain(bearer))
    expect(keys[0].status).toBe('active')
    expect(cachedData()).not.toContain(bearer)
  })
  it('retains delivery on revocation failure and retries dismissal without confirming', async () => {
    await createDelivery()
    failures[`delete ${base}/pky_new`] = 503
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]')!
        .click(),
    )
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(document.body.textContent).toContain(bearer)
    expect(keys[0].status).toBe('pending')
    delete failures[`delete ${base}/pky_new`]
    await click('Cancel and revoke')
    await until(() => expect(document.body.textContent).not.toContain(bearer))
    expect(keys[0].status).toBe('revoked')
    expect(requests.some((request) => request.url?.endsWith('/confirm'))).toBe(false)
  })
  it('retains original authority after delivery and retires only through explicit successful completion', async () => {
    keys = [fixtureKey()]
    await render()
    await until(() => expect(container.textContent).toContain('Production'))
    await click('Rotate')
    await until(() => expect(document.body.textContent).toContain(bearer))
    await acknowledge()
    await click('Confirm delivery')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(keys.map((key) => key.status)).toEqual(['active', 'active'])
    expect(requests.some((request) => request.url?.endsWith('/complete-rotation'))).toBe(false)
    const sourceRow = [...container.querySelectorAll('tbody tr')].find(
      (row) => row.textContent?.startsWith('Production') && !row.textContent.includes('Replaces'),
    )!
    await click('Complete rotation', sourceRow)
    await until(() =>
      expect(document.querySelector('option[value="pky_replacement"]')).not.toBeNull(),
    )
    await act(async () => {
      const select = document.querySelector<HTMLSelectElement>('select[name="replacement_key_id"]')!
      select.value = 'pky_replacement'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    failures[`post ${base}/pky_old/complete-rotation`] = 409
    await submit()
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(keys.find((key) => key.id === 'pky_old')!.status).toBe('active')
    delete failures[`post ${base}/pky_old/complete-rotation`]
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(keys.find((key) => key.id === 'pky_old')!.status).toBe('revoked')
    expect(
      JSON.parse(requests.find((request) => request.url?.endsWith('/complete-rotation'))!.data),
    ).toEqual({ replacement_key_id: 'pky_replacement' })
  })
  it('supports independent emergency revocation and sends only mutable metadata on rename', async () => {
    keys = [fixtureKey()]
    await render()
    await until(() => expect(container.textContent).toContain('Production'))
    await click('Rename')
    await fill('name', 'Renamed')
    await submit()
    await until(() => expect(container.textContent).toContain('Renamed'))
    expect(JSON.parse(requests.find((request) => request.method === 'patch')!.data)).toEqual({
      name: 'Renamed',
    })
    await click('Revoke')
    expect(requests.some((request) => request.method === 'delete')).toBe(false)
    await submit()
    await until(() => expect(keys[0].status).toBe('revoked'))
    expect(requests.some((request) => request.url?.endsWith('/rotate'))).toBe(false)
  })
  it('honors disabled and archived projects without hiding history', async () => {
    project.status = 'disabled'
    keys = [fixtureKey()]
    await render()
    await until(() => expect(container.textContent).toContain('Production'))
    expect(button('Create project key').disabled).toBe(true)
    expect(button('Rotate').disabled).toBe(true)
    expect(button('Disable').disabled).toBe(false)
    expect(button('Revoke').disabled).toBe(false)
    await click('Disable')
    await until(() => expect(keys[0].status).toBe('disabled'))
    await until(() => expect(button('Enable').disabled).toBe(true))
    project = { ...project, status: 'archived' }
    await render()
    expect(button('Rename').disabled).toBe(true)
    expect(button('Revoke').disabled).toBe(true)
    expect(button('Enable').disabled).toBe(true)
    expect(container.textContent).toContain('history is read-only')
  })
  it('loads cursor pages, filters on the server, and preserves immutable scope after grants are removed', async () => {
    keys = [fixtureKey(), fixtureKey('pky_two', 'revoked')]
    paginated = true
    await render()
    await until(() => expect(container.textContent).toContain('Production'))
    await click('Load more keys')
    await until(() => expect(container.querySelectorAll('tbody tr')).toHaveLength(2))
    expect(requests.some((request) => request.params?.cursor === 'cursor_next')).toBe(true)
    project = { ...project, model_ids: [] }
    await render()
    expect(button('Create project key').disabled).toBe(true)
    expect(container.textContent).toContain('This project has no model grants.')
    await click('1 model')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'This model grant was removed',
    )
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]')!
        .click(),
    )
    await act(async () => {
      const select = container.querySelector<HTMLSelectElement>('select')!
      select.value = 'revoked'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await until(() =>
      expect(requests.some((request) => request.params?.status === 'revoked')).toBe(true),
    )
  })
  it('validates scope, preserves drafts through live language changes, and never writes invalid input', async () => {
    await render()
    await until(() => expect(button('Create project key').disabled).toBe(false))
    await click('Create project key')
    await fill('name', 'Draft')
    await submit()
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      'Select at least one model.',
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      '请至少选择一个模型。',
    )
    expect(document.querySelector<HTMLInputElement>('input[name="name"]')?.value).toBe('Draft')
    expect(requests.some((request) => request.method === 'post')).toBe(false)
  })
  it('allows delegated project writers and keeps disabled-source rotations disabled after delivery', async () => {
    project.managers = [
      { id: 'pmg_other', user_id: 'usr_other', name: 'Other', email: 'other@example.test' },
    ]
    permissions = ['projects.write']
    keys = [fixtureKey('pky_old', 'disabled')]
    await render()
    await until(() => expect(container.textContent).toContain('Production'))
    await click('Rotate')
    await until(() => expect(document.body.textContent).toContain(bearer))
    await acknowledge()
    await click('Confirm delivery')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(keys.map((key) => key.status)).toEqual(['disabled', 'disabled'])
    const rotate = requests.find((request) => request.url?.endsWith('/rotate'))!
    expect(JSON.parse(rotate.data)).toEqual({ delivery_mode: 'manual' })
    expect(rotate.headers.get('X-CSRF-Token')).toBe('csrf')
  })
  it('does not offer activation or rotation for an expired key and retries failed list reads', async () => {
    keys = [{ ...fixtureKey('pky_old', 'disabled'), expires_at: '2020-01-01T00:00:00Z' }]
    failures[`get ${base}`] = 503
    await render()
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    delete failures[`get ${base}`]
    await click('Retry')
    await until(() => expect(container.textContent).toContain('Production'))
    expect(button('Enable').disabled).toBe(true)
    expect(button('Rotate').disabled).toBe(true)
    expect(button('Revoke').disabled).toBe(false)
    expect(container.textContent).toContain('Expired')
  })
  it('clears a local delivery if the project becomes archived without attempting a prohibited mutation', async () => {
    await createDelivery()
    project = { ...project, status: 'archived' }
    await render()
    expect(button('Confirm delivery').disabled).toBe(true)
    await act(async () =>
      document
        .querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]')!
        .click(),
    )
    await until(() => expect(document.body.textContent).not.toContain(bearer))
    expect(
      requests.some((request) => request.method === 'delete' || request.url?.endsWith('/confirm')),
    ).toBe(false)
  })
})
