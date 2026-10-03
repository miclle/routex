import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  TeamModelCandidate,
  TeamModelRequestDetail,
  TeamModelRequestTeam,
  TeamModelWorkspace,
} from '@/types/team-model-requests'
import {
  teamModelCandidate,
  teamModelDetail,
  teamModelTeam,
  teamModelTime,
  teamModelWorkspace,
} from './fixture'
import TeamAccessRequest from './request'
import TeamRequestPanel from './requests'
import Workspace from './index'
import AccessRequestFooter from './footer'
import ModelRequestHistory from './history'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const oldAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let auth: Session,
  teams: TeamModelRequestTeam[],
  candidates: Record<string, TeamModelCandidate>,
  rows: TeamModelRequestDetail[],
  workspace: TeamModelWorkspace
let requests: InternalAxiosRequestConfig[],
  failures: Record<string, number>,
  malformed: boolean,
  application: 'pending' | 'applied' | 'superseded'
let barrier: { pattern: string; promise: Promise<void>; release: () => void } | undefined
const busy = vi.fn(),
  locked = vi.fn()
function hold(pattern: string) {
  let release!: () => void
  barrier = {
    pattern,
    promise: new Promise<void>((resolve) => {
      release = resolve
    }),
    release: () => release(),
  }
  return barrier
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  auth = {
    user: { id: 'usr_owner', name: 'Owner', email: 'private@example.invalid', role: 'member' },
    csrf_token: 'csrf',
  }
  teams = [teamModelTeam(), teamModelTeam('tea_other', 'tmm_other')]
  candidates = { tea_zero: teamModelCandidate(), tea_other: teamModelCandidate('tea_other') }
  rows = [teamModelDetail()]
  workspace = teamModelWorkspace()
  requests = []
  failures = {}
  malformed = false
  application = 'pending'
  barrier = undefined
  busy.mockReset()
  locked.mockReset()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const url = config.url!,
      body = config.data ? JSON.parse(config.data) : undefined
    if (config.method === 'get' && barrier && url.includes(barrier.pattern)) await barrier.promise
    const response = {
      config,
      status: failures[`${config.method} ${url}`] ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    if (url === '/auth/session') response.data = structuredClone(auth)
    else if (url === '/auth/permissions') response.data = { permissions: [] }
    else if (url === '/team-model-request-teams')
      response.data = {
        items: teams.filter((item) =>
          item.name.toLowerCase().includes(String(config.params.q ?? '').toLowerCase()),
        ),
        next_cursor: null,
      }
    else if (url.includes('/model-request-candidates/'))
      response.data = structuredClone(candidates[url.split('/')[2]])
    else if (url.endsWith('/model-request-workspace'))
      response.data = { ...structuredClone(workspace), team_id: url.split('/')[2] }
    else if (
      config.method === 'get' &&
      (url.endsWith('/model-requests') || url === '/team-model-requests')
    ) {
      const visible = rows.filter(
        (row) =>
          (!url.startsWith('/teams/') || row.team_id === url.split('/')[2]) &&
          (!config.params.team_id || row.team_id === config.params.team_id) &&
          (!config.params.model_id || row.model_id === config.params.model_id) &&
          (!config.params.status || row.status === config.params.status),
      )
      response.data = { items: structuredClone(visible), total: visible.length, next_cursor: null }
    } else if (config.method === 'get' && url.endsWith('/tmr_request'))
      response.data = structuredClone(rows[0])
    else if (url === '/personal-model-requests')
      response.data = { items: [], total: 0, next_cursor: null }
    else if (url.startsWith('/model-access-candidates/'))
      response.data = {
        id: 'mdl_model',
        name: 'Model',
        status: 'active',
        created_at: teamModelTime,
        protocols: ['openai_chat'],
        input_capabilities: {},
        personal_granted: false,
        pending_request_id: null,
        review_etag: 'a'.repeat(64),
      }
    else if (url === '/team-model-requests' && config.method === 'post') {
      const row = {
        ...teamModelDetail(body.team_id),
        request_id: body.request_id,
        reason: body.reason,
        applicant_membership_id:
          teams.find((item) => item.id === body.team_id)?.membership_id ?? 'tmm_first',
      }
      rows = [row]
      candidates[body.team_id] = {
        ...candidates[body.team_id],
        pending_request: true,
        own_pending_request_id: row.id,
      }
      response.data = malformed ? {} : structuredClone(row)
    } else if (config.method === 'post' && url.endsWith('/decision')) {
      const row = rows[0],
        status =
          body.action === 'approve'
            ? 'approved'
            : body.action === 'reject'
              ? 'rejected'
              : 'withdrawn'
      rows = [
        {
          ...row,
          status,
          resolved_at: teamModelTime,
          allowed_actions: [],
          current_granted:
            row.application_status === 'unavailable'
              ? null
              : body.action === 'approve' && application !== 'superseded',
          runtime_applied:
            row.application_status === 'unavailable'
              ? null
              : body.action === 'approve' && application === 'applied',
          application_status:
            row.application_status === 'unavailable'
              ? 'unavailable'
              : body.action === 'approve'
                ? application
                : 'pending',
          decision: {
            ...body,
            actor_id: auth.user.id,
            actor_name: auth.user.name,
            decided_at: teamModelTime,
          },
        },
      ]
      response.data = malformed
        ? {}
        : {
            decision_id: body.decision_id,
            committed: true,
            saved_request: rows[0],
            current_granted: rows[0].current_granted,
            runtime_applied: rows[0].runtime_applied,
            current_membership_matches: rows[0].current_membership_matches,
            application_status: rows[0].application_status,
          }
    } else throw new Error(`Unexpected request ${config.method} ${url}`)
    return response
  }
})
afterEach(async () => {
  barrier?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let attempt = 0; attempt < 120; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (attempt === 119) throw error
    }
  }
}
async function mount(component: ReactNode) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>{component}</MemoryRouter>
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')]
    .filter((item) => item.textContent === label)
    .at(-1)!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function select(label: string, value: string) {
  const input = document.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function type(value: string) {
  const input = document.querySelector('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function search(value: string) {
  const input = document.querySelector<HTMLInputElement>('input[type="search"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const posts = () => requests.filter((request) => request.method === 'post')
async function mountRequest() {
  await mount(
    <TeamAccessRequest actorID="usr_owner" modelID="mdl_model" onBusy={busy} onLocked={locked} />,
  )
  await until(() =>
    expect(document.querySelector('select option[value="tea_zero"]')).not.toBeNull(),
  )
}
async function chooseTeam(team = 'tea_zero') {
  await select('Team', team)
  await until(() => expect(button('Submit Team request')).toBeDefined())
}
async function reviewRequest() {
  auth.user.id = 'usr_reviewer'
  await mount(<TeamRequestPanel team="tea_zero" />)
  await until(() => expect(button('Team request details')).toBeDefined())
  await click('Team request details')
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Original Team need'),
  )
}

describe('Team single-Model request interactions', () => {
  it('requires an explicit zero-grant Team selection and only fetches minimal membership/candidate APIs', async () => {
    rows = []
    await mountRequest()
    expect(document.querySelector<HTMLSelectElement>('select[aria-label="Team"]')?.value).toBe('')
    expect(button('Submit Team request')).toBeUndefined()
    expect(requests.some((request) => request.url?.includes('model-request-candidates'))).toBe(
      false,
    )
    await chooseTeam()
    await type('Shared research')
    await click('Submit Team request')
    await until(() => expect(host.textContent).toContain('Team model access requested'))
    expect(JSON.parse(posts()[0].data)).toMatchObject({
      team_id: 'tea_zero',
      model_id: 'mdl_model',
      reason: 'Shared research',
    })
    expect(posts()[0].headers.get('If-Match')).toBe('"' + 'a'.repeat(64) + '"')
    expect(
      requests.some(
        (request) =>
          request.url === '/teams' ||
          request.url === '/admin/teams' ||
          request.url?.endsWith('/roles'),
      ),
    ).toBe(false)
    expect(host.textContent).not.toContain('currently applied')
    const sessionReads = requests.filter((request) => request.url === '/auth/session').length
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)))
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(sessionReads)
  })
  it('preserves bounded Team selection across an unrelated search without changing its target', async () => {
    await mountRequest()
    await chooseTeam()
    await type('Draft')
    await search('Other')
    await until(() => expect(document.querySelector('option[value="tea_other"]')).not.toBeNull())
    expect(document.querySelector<HTMLSelectElement>('select[aria-label="Team"]')?.value).toBe(
      'tea_zero',
    )
    expect(document.querySelector('option[value="tea_zero"]')?.textContent).toBe('Zero grants Team')
    await click('Submit Team request')
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).team_id).toBe('tea_zero')
  })
  it('requires explicit review when the same Team returns a different membership generation', async () => {
    await mountRequest()
    await chooseTeam()
    await type('Preserved rejoin draft')
    teams = [teamModelTeam('tea_zero', 'tmm_rejoined')]
    candidates.tea_zero.review_etag = 'b'.repeat(64)
    await click('Refresh Teams')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-candidate'] })
    })
    await until(() => expect(button('Review current state')).toBeDefined())
    expect(button('Submit Team request').disabled).toBe(true)
    expect(document.querySelector('textarea')?.value).toBe('Preserved rejoin draft')
    expect(posts()).toHaveLength(0)
    await click('Review current state')
    await until(() => expect(button('Submit Team request')?.disabled).toBe(false))
    await click('Submit Team request')
    await until(() => expect(posts()).toHaveLength(1))
    expect(posts()[0].headers.get('If-Match')).toBe('"' + 'b'.repeat(64) + '"')
    expect(
      cache.getQueryData([
        'team-model-candidate',
        'usr_owner',
        'tea_zero',
        'tmm_rejoined',
        'mdl_model',
      ]),
    ).toBeDefined()
  })
  it('retains exact unknown creation through a candidate 403 and a rejected original retry', async () => {
    failures['post /team-model-requests'] = 503
    await mountRequest()
    await chooseTeam()
    await type('Immutable Team intent')
    await click('Submit Team request')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const first = posts()[0]
    expect(document.querySelector<HTMLSelectElement>('select[aria-label="Team"]')?.disabled).toBe(
      true,
    )
    expect(button('Review current state')).toBeUndefined()
    failures['get /teams/tea_zero/model-request-candidates/mdl_model'] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-candidate'] })
    })
    await until(() => expect(button('Retry original request').disabled).toBe(true))
    expect(posts()).toHaveLength(1)
    delete failures['get /teams/tea_zero/model-request-candidates/mdl_model']
    failures['post /team-model-requests'] = 409
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-candidate'] })
    })
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(host.textContent).toContain('does not resolve uncertainty')
    expect(locked).toHaveBeenLastCalledWith(true)
  })
  it('hides selected Team and candidate details through picker renewal/errors without discarding the draft', async () => {
    await mountRequest()
    await chooseTeam()
    await type('Retained picker draft')
    const pending = hold('/team-model-request-teams')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-request-teams'] })
    })
    await until(() => expect(button('Submit Team request')).toBeUndefined())
    expect(document.querySelector('option[value="tea_zero"]')).toBeNull()
    failures['get /team-model-request-teams'] = 403
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(button('Submit Team request')).toBeUndefined()
    expect(posts()).toHaveLength(0)
    delete failures['get /team-model-request-teams']
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-request-teams'] })
    })
    await until(() => expect(button('Submit Team request')).toBeDefined())
    expect(document.querySelector('textarea')?.value).toBe('Retained picker draft')
  })
  it('ignores an obsolete exact Team response after switching the selected target', async () => {
    await mountRequest()
    const pending = hold('/teams/tea_zero/model-request-candidates/')
    await select('Team', 'tea_zero')
    await until(() =>
      expect(
        requests.some((item) => item.url === '/teams/tea_zero/model-request-candidates/mdl_model'),
      ).toBe(true),
    )
    await select('Team', 'tea_other')
    await until(() => expect(button('Submit Team request')).toBeDefined())
    await act(async () => pending.release())
    barrier = undefined
    await type('Exact other Team')
    await click('Submit Team request')
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).team_id).toBe('tea_other')
  })
  it('keeps a malformed saved response uncertain and preserves its exact creation request', async () => {
    malformed = true
    await mountRequest()
    await chooseTeam()
    await type('Original body')
    await click('Submit Team request')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const original = posts()[0]
    expect(host.textContent).not.toContain('Team model access requested')
    malformed = false
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    await until(() => expect(host.textContent).toContain('Team model access requested'))
  })
  it('keeps the Team draft through live bilingual switching', async () => {
    await mountRequest()
    await chooseTeam()
    await type('保留的 Team 理由')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('textarea')?.value).toBe('保留的 Team 理由')
    expect(button('提交 Team 申请')).toBeDefined()
    expect(host.textContent).toContain('审批仅增加所选 Team 的模型授权')
    expect(posts()).toHaveLength(0)
  })
  it('keeps Personal as the default footer scope and permits explicit Team choice even with Personal access', async () => {
    await mount(
      <AccessRequestFooter
        actorID="usr_owner"
        modelID="mdl_model"
        personalGranted
        visible
        onBusy={busy}
      />,
    )
    expect(
      document.querySelector<HTMLSelectElement>('select[aria-label="Request scope"]')?.value,
    ).toBe('personal')
    expect(requests).toHaveLength(0)
    await select('Request scope', 'team')
    await until(() => expect(document.querySelector('option[value="tea_zero"]')).not.toBeNull())
    expect(requests.some((request) => request.url === '/team-model-request-teams')).toBe(true)
    expect(requests.some((request) => request.url?.startsWith('/model-access-candidates'))).toBe(
      false,
    )
  })
  it('provides own historical Team requests after leaving every Team without a directory fetch', async () => {
    teams = []
    rows = [
      {
        ...rows[0],
        current_team: null,
        current_model: null,
        current_granted: null,
        current_membership_matches: null,
        runtime_applied: null,
        application_status: 'unavailable',
        allowed_actions: ['withdraw'],
      },
    ]
    await mount(<ModelRequestHistory actor="usr_owner" visible />)
    const tab =
      document.querySelector<HTMLButtonElement>('[role="tab"][data-value="team"]') ??
      [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(
        (item) => item.textContent === 'Team',
      )!
    await act(async () => tab.click())
    await until(() => expect(button('Team request details')).toBeDefined())
    await click('Team request details')
    await until(() =>
      expect(document.body.textContent).toContain('Current Team access facts are unavailable'),
    )
    expect(button('Withdraw')).toBeDefined()
    expect(button('Approve')).toBeUndefined()
    expect(
      requests.every(
        (request) =>
          !request.url?.startsWith('/teams/') &&
          request.url !== '/teams' &&
          request.url !== '/team-model-request-teams',
      ),
    ).toBe(true)
    await click('Withdraw')
    await click('Withdraw')
    await until(() => expect(posts()).toHaveLength(1))
    expect(posts()[0].url).toBe('/team-model-requests/tmr_request/decision')
    await until(() => expect(document.body.textContent).toContain('The decision was committed'))
  })
  it('retains own unknown withdrawal in the model footer and locks its parent/history controls', async () => {
    await mountRequest()
    await chooseTeam()
    await click('My Team model requests')
    await until(() => expect(button('Team request details')).toBeDefined())
    await click('Team request details')
    await until(() => expect(button('Withdraw')).toBeDefined())
    await click('Withdraw')
    failures['post /team-model-requests/tmr_request/decision'] = 503
    await click('Withdraw')
    await until(() => expect(button('Retry original request')).toBeDefined())
    expect(button('My Team model requests').disabled).toBe(true)
    expect(busy).toHaveBeenLastCalledWith(true)
    expect(locked).toHaveBeenLastCalledWith(true)
    expect(posts()).toHaveLength(1)
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <TeamAccessRequest
              actorID="usr_owner"
              modelID="mdl_model"
              onBusy={busy}
              onLocked={locked}
              visible={false}
            />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <TeamAccessRequest
              actorID="usr_owner"
              modelID="mdl_model"
              onBusy={busy}
              onLocked={locked}
            />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Retry original request')).toBeDefined())
    expect(posts()).toHaveLength(1)
  })
  it('loads an independent reviewer exact URL with only minimal grants and scoped requests', async () => {
    auth.user.id = 'usr_reviewer'
    const router = createMemoryRouter(
      [{ path: '/teams/:resourceId/model-requests', element: <Workspace /> }],
      { initialEntries: ['/teams/tea_zero/model-requests'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Team request details')).toBeDefined())
    expect(host.textContent).toContain('No current Team model grants')
    expect(host.textContent).not.toContain('private@example.invalid')
    expect(
      requests.every(
        (request) =>
          request.url === '/auth/session' ||
          /\/(model-request-workspace|model-requests)$/.test(request.url!),
      ),
    ).toBe(true)
    await click('Team request details')
    await until(() => expect(button('Approve')).toBeDefined())
    await click('Approve')
    expect(posts()).toHaveLength(0)
    await click('Approve')
    await until(() => expect(document.body.textContent).toContain('The decision was committed'))
    expect(document.body.textContent).toContain('runtime application is pending')
    router.dispose()
  })
  it('never treats applicant/owner identity as review authority and never permits self review', async () => {
    await mount(<TeamRequestPanel team="tea_zero" />)
    await until(() => expect(button('Team request details')).toBeDefined())
    await click('Team request details')
    await until(() => expect(document.body.textContent).toContain('Another authorized reviewer'))
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeUndefined()
    expect(posts()).toHaveLength(0)
  })
  it('hides reviewer history/details during authority renewal and preserves an unknown decision through revocation', async () => {
    await reviewRequest()
    await click('Approve')
    failures['post /teams/tea_zero/model-requests/tmr_request/decision'] = 503
    await click('Approve')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const first = posts()[0],
      pending = hold('/model-request-workspace')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-workspace'] })
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).not.toContain('Original Team need')
    failures['get /teams/tea_zero/model-request-workspace'] = 403
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(posts()).toHaveLength(1)
    delete failures['get /teams/tea_zero/model-request-workspace']
    failures['post /teams/tea_zero/model-requests/tmr_request/decision'] = 409
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-workspace'] })
    })
    await until(() => expect(button('Retry original request')?.disabled).toBe(false))
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(document.body.textContent).toContain('does not resolve uncertainty')
  })
  it('requires explicit detail ETag review and preserves rejection reason on changed current facts', async () => {
    await reviewRequest()
    await click('Reject')
    await type('Specific rejection')
    const pending = hold('/tmr_request')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-request'] })
    })
    await until(() => expect(document.querySelector('[role="dialog"] dd')).toBeNull())
    rows[0].review_etag = 'b'.repeat(64)
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(button('Review current state')).toBeDefined())
    expect(document.querySelector('textarea')?.value).toBe('Specific rejection')
    expect(button('Reject').disabled).toBe(true)
    await click('Review current state')
    await click('Reject')
    await until(() => expect(posts()).toHaveLength(1))
    expect(posts()[0].headers.get('If-Match')).toBe('"' + 'b'.repeat(64) + '"')
  })
  it('separates a committed shared grant from applicant membership and refreshed superseded application', async () => {
    application = 'applied'
    rows[0].current_membership_matches = false
    await reviewRequest()
    await click('Approve')
    await click('Approve')
    await until(() =>
      expect(document.body.textContent).toContain('The recorded Team grant is currently applied'),
    )
    expect(document.body.textContent).toContain('may remain after the applicant leaves')
    rows[0] = {
      ...rows[0],
      current_granted: false,
      runtime_applied: false,
      application_status: 'superseded',
    }
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-model-request'] })
    })
    await until(() =>
      expect(document.body.textContent).toContain('The recorded approval was superseded'),
    )
    expect(document.body.textContent).toContain('The decision was committed')
    expect(document.body.textContent).not.toContain('currently applied')
    expect(posts()).toHaveLength(1)
  })
  it('hides all old Team details across account change and never dispatches a retained old actor intent', async () => {
    await reviewRequest()
    await click('Approve')
    auth = { ...auth, user: { ...auth.user, id: 'usr_next' } }
    failures['get /teams/tea_zero/model-request-workspace'] = 403
    await act(async () => cache.setQueryData(sessionKey, auth))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).not.toContain('Original Team need')
    expect(posts()).toHaveLength(0)
  })
})
