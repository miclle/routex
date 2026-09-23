import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/projectRequests'
import zh from '@/i18n/locales/zh/projectRequests'
import type { ResourceRecord } from '@/types/resources'
import type { ProjectRequest } from '@/types/project-requests'
import ProjectRequestsPanel from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, container: HTMLDivElement, cache: QueryClient
let project: ResourceRecord, record: ProjectRequest
let requests: InternalAxiosRequestConfig[],
  permissions: string[],
  failure: number,
  paginate: boolean
const oldAdapter = client.defaults.adapter
const path = '/projects/prj_one/requests'
beforeEach(async () => {
  i18n.addResourceBundle('en', 'projectRequests', en, true, true)
  i18n.addResourceBundle('zh', 'projectRequests', zh, true, true)
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['projects.models.write']
  failure = 0
  paginate = false
  project = {
    id: 'prj_one',
    name: 'Research',
    description: '',
    status: 'active',
    created_at: '2026-09-23T00:00:00Z',
    model_ids: ['mdl_baseline'],
    managers: [{ id: 'pm_one', user_id: 'usr_me', name: 'Manager', email: 'me@example.invalid' }],
  }
  record = {
    id: 'req_one',
    project_id: project.id,
    applicant_user_id: 'usr_other',
    kind: 'MODEL_ACCESS',
    baseline_model_ids: ['mdl_old'],
    requested_model_ids: ['mdl_new'],
    reason: 'Research workload',
    status: 'pending',
    created_at: '2026-09-23T00:00:00Z',
    decided_at: null,
  }
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: { id: 'usr_me', name: 'Manager', email: 'me@example.invalid', role: 'admin' },
        csrf_token: 'csrf-test',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.method === 'post') {
      if (failure) {
        response.status = failure
        throw new AxiosError('Failed', '', config, undefined, response)
      }
      const body = JSON.parse(config.data)
      if (config.url === path) {
        record = {
          ...record,
          applicant_user_id: 'usr_me',
          requested_model_ids: body.model_ids,
          reason: body.reason,
          baseline_model_ids: [...project.model_ids],
        }
      } else {
        record.status = { approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' }[
          body.action as 'approve' | 'reject' | 'withdraw'
        ] as ProjectRequest['status']
        record.decision_actor_id = 'usr_me'
        record.decision_reason = body.reason
        record.decided_at = '2026-09-23T01:00:00Z'
      }
      response.data = structuredClone(record)
    } else if (config.url === path) {
      const item = config.params?.cursor
        ? { ...record, id: 'req_older', reason: 'Older history' }
        : record
      response.data = {
        items:
          !config.params?.status || item.status === config.params.status
            ? [structuredClone(item)]
            : [],
        next_cursor: paginate && !config.params?.cursor ? 'req_cursor' : null,
      }
    } else if (config.url === '/projects/prj_one/request-model-candidates')
      response.data = { items: [{ id: 'mdl_new', name: 'Research Model' }] }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  client.defaults.adapter = oldAdapter
  container.remove()
})
async function until(assert: () => void) {
  for (let i = 0; i < 80; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 79) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ProjectRequestsPanel project={project} />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(requests.some((r) => r.url === '/auth/permissions')).toBe(true))
  await until(() => expect(document.body.textContent).not.toContain('Loading'))
}
function button(text: string, scope: ParentNode = document) {
  const found = [...scope.querySelectorAll('button')].find((b) => b.textContent === text)
  expect(found, `button ${text}`).toBeTruthy()
  return found!
}
async function click(element: HTMLElement) {
  await act(async () => element.click())
}
async function fill(selector: string, value: string) {
  await act(async () => {
    const input = document.querySelector<
      HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement
    >(selector)!
    expect(input).toBeTruthy()
    const proto =
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : input instanceof HTMLSelectElement
          ? HTMLSelectElement.prototype
          : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(input, value)
    input.dispatchEvent(
      new Event(input instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
    )
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
const writes = () => requests.filter((r) => r.method === 'post')
async function review() {
  await until(() => expect(document.body.textContent).toContain('Research workload'))
  await click(button('Review request'))
}

describe('Project model requests', () => {
  it('submits explicit additions only and preserves request identity across an uncertain result', async () => {
    await mount()
    await click(button('Request models'))
    await until(() => expect(document.body.textContent).toContain('Research Model'))
    await fill('input', 'Research')
    await until(() =>
      expect(
        requests.some(
          (r) => r.url?.endsWith('request-model-candidates') && r.params.q === 'Research',
        ),
      ).toBe(true),
    )
    await click(button('Research Model'))
    await fill('textarea', '  Need inference access  ')
    failure = 503
    await submit()
    await until(() => expect(document.body.textContent).toContain(en.unavailable))
    const first = JSON.parse(writes()[0].data)
    expect(first).toMatchObject({
      kind: 'MODEL_ACCESS',
      model_ids: ['mdl_new'],
      reason: 'Need inference access',
    })
    expect(first.request_id).toBeTruthy()
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-test')
    expect(document.querySelector('textarea')!.readOnly).toBe(true)
    failure = 0
    await click(button('Retry the same action'))
    await until(() => expect(document.body.textContent).toContain(en.saved))
    expect(JSON.parse(writes()[1].data)).toEqual(first)
    expect(project.model_ids).toEqual(['mdl_baseline'])
    expect(requests.some((r) => r.url === '/models')).toBe(false)
  })

  it('requires an actual manager to apply even when the reader has platform approval permission', async () => {
    project.managers = []
    await mount()
    await review()
    expect(document.body.textContent).toContain(en.managerOnly)
    expect([...document.querySelectorAll('button')].some((b) => b.textContent === en.apply)).toBe(
      false,
    )
    expect(button('Approve')).toBeTruthy()
    expect(requests.some((r) => r.url?.endsWith('request-model-candidates'))).toBe(false)
  })

  it('does not load scoped history without a relationship or read permission', async () => {
    project.managers = []
    permissions = []
    await mount()
    await until(() => expect(document.body.textContent).toContain(en.deniedRead))
    expect(requests.some((r) => r.url === path)).toBe(false)
  })

  it('prevents self approval and permits own withdrawal on an inactive Project', async () => {
    record.applicant_user_id = 'usr_me'
    project.status = 'archived'
    await mount()
    await review()
    expect(button('Request models').disabled).toBe(true)
    expect(document.body.textContent).toContain(en.selfApproval)
    expect(
      [...document.querySelectorAll('button')].some(
        (b) => b.textContent === en.approve || b.textContent === en.reject,
      ),
    ).toBe(false)
    await click(button('Withdraw'))
    await click(button('Confirm Withdraw'))
    await until(() => expect(document.body.textContent).toContain(en.decided))
    expect(JSON.parse(writes()[0].data)).toEqual({ action: 'withdraw', reason: '' })
  })

  it('requires a rejection reason and displays the immutable decision history', async () => {
    project.status = 'disabled'
    await mount()
    await review()
    expect([...document.querySelectorAll('button')].some((b) => b.textContent === en.approve)).toBe(
      false,
    )
    await click(button('Reject'))
    expect(button('Confirm Reject').disabled).toBe(true)
    await submit()
    expect(writes()).toHaveLength(0)
    await fill('textarea', 'Use the existing approved model')
    await click(button('Confirm Reject'))
    await until(() => expect(document.body.textContent).toContain(en.decided))
    await review()
    expect(document.body.textContent).toContain('Use the existing approved model')
    expect(document.body.textContent).toContain('usr_me')
    expect(document.querySelector('form')).toBeNull()
    expect(JSON.parse(writes()[0].data).action).toBe('reject')
  })

  it('keeps approval intent for uncertain retries and refreshes a competing decision on conflict', async () => {
    await mount()
    await review()
    await click(button('Approve'))
    await fill('textarea', 'Reviewed additions')
    failure = 503
    await click(button('Confirm Approve'))
    await until(() => expect(document.body.textContent).toContain(en.unavailable))
    expect(button('Reject').disabled).toBe(true)
    failure = 409
    record.status = 'rejected'
    record.decision_actor_id = 'usr_reviewer'
    record.decision_reason = 'Already reviewed'
    record.decided_at = '2026-09-23T01:00:00Z'
    await click(button('Retry the same action'))
    await until(() => expect(document.body.textContent).toContain(en.conflict))
    expect(JSON.parse(writes()[1].data)).toEqual(JSON.parse(writes()[0].data))
    await click(button('Refresh history', document.querySelector('[role="dialog"]')!))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await review()
    expect(document.body.textContent).toContain('Already reviewed')
    expect(document.querySelector('form')).toBeNull()
  })

  it('saves a nonself approval and refreshes history without changing the baseline into a grant payload', async () => {
    await mount()
    await review()
    await click(button('Approve'))
    await click(button('Confirm Approve'))
    await until(() => expect(document.body.textContent).toContain(en.decided))
    expect(JSON.parse(writes()[0].data)).toEqual({ action: 'approve', reason: '' })
    await review()
    expect(document.body.textContent).toContain(en.approved)
    expect(document.body.textContent).toContain('mdl_old')
    expect(document.body.textContent).toContain('mdl_new')
    expect(document.querySelector('form')).toBeNull()
  })

  it('allows a manager to read and apply without granting approval controls', async () => {
    permissions = []
    await mount()
    await review()
    expect(button('Request models')).toBeTruthy()
    expect(
      [...document.querySelectorAll('button')].some(
        (b) =>
          b.textContent === en.approve ||
          b.textContent === en.reject ||
          b.textContent === en.withdraw,
      ),
    ).toBe(false)
    expect(document.querySelector('form')).toBeNull()
  })

  it('scopes pagination and status filters and changes both UI locales', async () => {
    paginate = true
    await mount()
    await until(() => expect(document.body.textContent).toContain(en.more))
    await click(button(en.more))
    await until(() => expect(document.body.textContent).toContain('Older history'))
    expect(
      requests.some(
        (r) => r.url === path && r.params.cursor === 'req_cursor' && r.params.limit === 40,
      ),
    ).toBe(true)
    await fill('select', 'approved')
    await until(() => expect(document.body.textContent).toContain(en.empty))
    expect(
      requests.some((r) => r.url === path && r.params.status === 'approved' && !r.params.cursor),
    ).toBe(true)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain(zh.title)
    expect(document.body.textContent).toContain(zh.empty)
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort())
    expect(Object.values(zh).every((value) => value.trim().length > 0)).toBe(true)
  })
})
