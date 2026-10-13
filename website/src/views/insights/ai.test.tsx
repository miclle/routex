import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import InsightsPage from './index'
import { usageTime } from '@/views/usage/format'
import type { InsightsAnalysis } from '@/types/insights'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const first = 'mdl_01arz3ndektsv4rrffq69g5fav',
  second = 'mdl_01arz3ndektsv4rrffq69g5faw'
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  actor: string,
  csrf: string,
  allowed: boolean,
  providerAllowed: boolean,
  errorStatus: number,
  completion: InsightsAnalysis['completion']
let requests: InternalAxiosRequestConfig[], gates: ReturnType<typeof deferred>[]
let aiGate: ReturnType<typeof deferred> | undefined,
  modelGate: ReturnType<typeof deferred> | undefined,
  sessionGate: ReturnType<typeof deferred> | undefined
function deferred(): { promise: Promise<void>; release: () => void } {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
function session() {
  return { user: { id: actor, role: 'member' }, csrf_token: csrf }
}
const text = (key: string) => i18n.t('insights:' + key)
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  csrf = 'fresh-csrf'
  allowed = true
  providerAllowed = false
  errorStatus = 0
  completion = 'completed'
  requests = []
  gates = []
  aiGate = modelGate = sessionGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/insights/reports') data = { items: [], next_cursor: '' }
    else if (config.url === '/auth/session') {
      data = structuredClone(session())
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions')
      data = {
        permissions: allowed
          ? ['calls.read_all', ...(providerAllowed ? ['providers.read'] : [])]
          : [],
      }
    else if (config.url === '/admin/insights/models') {
      if (modelGate) await modelGate.promise
      data = {
        items: [
          { model_id: first, name: 'Personal Chat' },
          { model_id: second, name: 'Other Chat' },
        ],
      }
    } else if (config.url === '/admin/insights/analyze') {
      const body = JSON.parse(config.data)
      if (aiGate) await aiGate.promise
      if (errorStatus)
        throw new AxiosError('private-provider-secret', '', config, undefined, {
          config,
          status: errorStatus,
          statusText: '',
          headers: new AxiosHeaders(),
          data: { message: 'private-provider-secret' },
        })
      data = {
        request_id: 'req_01arz3ndektsv4rrffq69g5fav',
        completion,
        text:
          completion === 'completed'
            ? '<script>alert(1)</script> ' +
              (body.report === 'provider_quality' ? 'P1' : 'M1') +
              ' advisory'
            : null,
        capture: {
          source:
            body.report === 'provider_quality'
              ? 'persisted_call_attempts'
              : 'persisted_call_records',
          from: body.from,
          to: body.to,
          timezone: body.timezone,
          queried_at: '2026-09-03T00:00:00Z',
          latest_completed_at: '2026-09-02T23:00:00Z',
          may_lag: true,
        },
        labels:
          body.report === 'provider_quality'
            ? [
                {
                  label: 'P1',
                  provider_id: 'historical-provider',
                  name: '<img src=provider>',
                  unknown: false,
                },
              ]
            : [{ label: 'M1', model_id: 'historical-model', name: '<img src=x>', unknown: false }],
      }
    } else throw Error('Unexpected endpoint ' + config.url)
    return { config, data, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let n = 0; n < 100; n++) {
    try {
      check()
      return
    } catch {
      await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    }
  }
  check()
}
function button(label: string) {
  const found = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(
    (b) => b.textContent?.trim() === label,
  )
  if (!found) throw Error('No button ' + label)
  return found
}
async function click(key: string) {
  await act(async () => button(text(key)).click())
}
async function input(name: string, value: string) {
  const field = host.querySelector<HTMLInputElement>('[name="' + name + '"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select(value: string) {
  const field = host.querySelector<HTMLSelectElement>('[aria-label="' + text('aiModel') + '"]')!
  await act(async () => {
    field.value = value
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <InsightsPage />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Personal Chat'))
  await input('from', '2026-09-01T00:00:00Z')
  await input('to', '2026-09-02T00:00:00Z')
  await select(first)
}
const posts = () => requests.filter((r) => r.url === '/admin/insights/analyze')
async function launch() {
  await click('aiReview')
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  await click('aiConfirm')
}

it('requires explicit personal model review and confirmation; forwards only fixed fields with current CSRF', async () => {
  await mount()
  expect(posts()).toHaveLength(0)
  await click('aiReview')
  expect(posts()).toHaveLength(0)
  expect(document.body.textContent).toContain(text('aiConfirmDescription'))
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(posts()).toHaveLength(1)
  expect(JSON.parse(posts()[0].data)).toEqual({
    report: 'tokens_by_model',
    model_id: first,
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    timezone: 'UTC',
    language: 'en',
  })
  expect(posts()[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(host.querySelector('script')).toBeNull()
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).toContain('<img src=x>')
  expect(host.textContent).toContain(text('aiCompletion_completed'))
  expect(host.textContent).toContain(usageTime('2026-09-03T00:00:00Z', 'en-US', 'UTC'))
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(
    cache
      .getQueryCache()
      .getAll()
      .some((q) => (JSON.stringify(q.state.data) ?? '').includes('M1 advisory')),
  ).toBe(false)
})
it('locks duplicate confirmation, keeps submitted language immutable and live-localizes pending copy', async () => {
  await mount()
  aiGate = deferred()
  await click('aiReview')
  await act(async () => {
    const confirm = button(text('aiConfirm'))
    confirm.click()
    confirm.click()
  })
  expect(posts()).toHaveLength(1)
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('aiPending'))
  await act(async () => aiGate!.release())
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data).language).toBe('en')
  expect(host.textContent).toContain(i18n.t('insights:aiLanguage', { language: text('aiEnglish') }))
})
it.each(['unknown', 'handoff', 'blocked', 'incomplete'] as const)(
  'shows native %s honestly without answer',
  async (state) => {
    completion = state
    await mount()
    await launch()
    await until(() => expect(host.textContent).toContain(text('aiCompletion_' + state)))
    expect(host.textContent).not.toContain('M1 advisory')
    expect(posts()).toHaveLength(1)
  },
)
it.each(
  (
    ['model', 'range', 'model list', 'actor', 'Session renewal', 'permission', 'cancel'] as const
  ).flatMap((change) => [false, true].map((withQuestion) => ({ change, withQuestion }))),
)(
  'aborts and rejects late AI after $change changes (question: $withQuestion)',
  async ({ change, withQuestion }) => {
    await mount()
    if (withQuestion) {
      await questionInput('Transient question on authorized aggregates')
      await click('send')
    }
    aiGate = deferred()
    await launch()
    const old = aiGate,
      signal = posts()[0].signal!
    if (change === 'model') await select(second)
    else if (change === 'range') await input('to', '2026-09-04T00:00:00Z')
    else if (change === 'model list') {
      modelGate = deferred()
      await act(async () => {
        void cache.refetchQueries({ queryKey: ['insights-models'] })
      })
    } else if (change === 'actor') {
      actor = 'usr_two'
      await act(async () => {
        cache.setQueryData(sessionKey, session())
      })
    } else if (change === 'Session renewal') {
      sessionGate = deferred()
      await act(async () => {
        void cache.refetchQueries({ queryKey: sessionKey })
      })
    } else if (change === 'permission') {
      allowed = false
      await act(async () => {
        void cache.refetchQueries({ queryKey: ['permissions'] })
      })
    } else await click('aiCancel')
    expect(signal.aborted).toBe(true)
    await act(async () => old.release())
    expect(host.textContent).not.toContain('M1 advisory')
    expect(posts()).toHaveLength(1)
  },
)
it.each([403, 404, 422, 429, 502, 503])(
  'sanitizes HTTP%i and never automatically replays paid inference',
  async (status) => {
    errorStatus = status
    await mount()
    await launch()
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('private-provider-secret')
    expect(host.textContent).not.toContain('M1 advisory')
    expect(posts()).toHaveLength(1)
  },
)
it('revokes a prepared confirmation on model change before any inference dispatch', async () => {
  await mount()
  await click('aiReview')
  await select(second)
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(posts()).toHaveLength(0)
})
async function questionInput(value: string) {
  const field = host.querySelector<HTMLTextAreaElement>('[name="question"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

it('reviews Provider quality and exact continued question without changing the fixed report', async () => {
  providerAllowed = true
  await mount()
  await click('shortcutProvider')
  expect(host.textContent).toContain(text('aiProviderDisclosure'))
  const question = '  Explain P1 failures\nwith unknown duration  '
  await questionInput(question)
  await click('send')
  await click('aiReview')
  const dialog = document.querySelector('[role="dialog"]')!
  expect(dialog.textContent).toContain(text('shortcutProvider'))
  expect(dialog.textContent).toContain(question)
  expect(posts()).toHaveLength(0)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('P1 advisory'))
  expect(JSON.parse(posts()[0].data)).toMatchObject({
    report: 'provider_quality',
    question,
    model_id: first,
    language: 'en',
  })
  expect(host.textContent).toContain(text('qualitySource'))
  expect(host.textContent).toContain(text('aiRecordedProvider'))
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).toContain('<img src=provider>')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('aiProviderLabels'))
  expect(host.textContent).toContain(text('aiRecordedProvider'))
  expect(host.textContent).toContain(question)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('requires independent Provider read before preparing an advisory request', async () => {
  await mount()
  await click('shortcutProvider')
  expect(host.textContent).toContain(text('providerReadDenied'))
  expect(host.querySelector('[aria-label="' + text('aiModel') + '"]')).toBeNull()
  await questionInput('Explain the selected Provider report')
  await click('send')
  expect(host.textContent).toContain(text('providerReadDenied'))
  expect(posts()).toHaveLength(0)
})

it.each(['shortcutTeam', 'shortcutKey'])(
  'keeps AI disabled for %s when a question is submitted',
  async (shortcut) => {
    await mount()
    await click(shortcut)
    await questionInput('Explain selected quota')
    await click('send')
    expect(host.textContent).toContain(text('unsupported'))
    expect(host.querySelector('[aria-label="' + text('aiModel') + '"]')).toBeNull()
    expect(host.querySelector<HTMLTextAreaElement>('[name="question"]')!.value).toBe(
      'Explain selected quota',
    )
    expect(posts()).toHaveLength(0)
  },
)

it.each(['review', 'pending', 'complete'] as const)(
  'invalidates obsolete Provider advice on report-kind change during %s',
  async (stage) => {
    providerAllowed = true
    await mount()
    await click('shortcutProvider')
    await click('aiReview')
    const oldConfirm = button(text('aiConfirm'))
    if (stage === 'pending') aiGate = deferred()
    if (stage !== 'review') await click('aiConfirm')
    if (stage === 'complete') await until(() => expect(host.textContent).toContain('P1 advisory'))
    await click('shortcutTokens')
    if (stage === 'review') await act(async () => oldConfirm.click())
    else if (stage === 'pending') {
      expect(posts()[0].signal!.aborted).toBe(true)
      await act(async () => aiGate!.release())
    }
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('P1 advisory')
    expect(posts()).toHaveLength(stage === 'review' ? 0 : 1)
  },
)

it('aborts Provider advice when only Provider-read authority is renewed and revoked', async () => {
  providerAllowed = true
  await mount()
  await click('shortcutProvider')
  aiGate = deferred()
  await launch()
  const gate = aiGate
  providerAllowed = false
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions'] })
  })
  expect(posts()[0].signal!.aborted).toBe(true)
  await act(async () => gate.release())
  expect(host.textContent).not.toContain('P1 advisory')
  expect(posts()).toHaveLength(1)
})

it('prepares exact free text without inference or database planning until personal-model confirmation', async () => {
  await mount()
  const question = '  SELECT secrets; <img src=x>\nExplain M1\tplease  '
  await questionInput(question)
  expect(button(text('aiReview')).disabled).toBe(true)
  await click('send')
  expect(posts()).toHaveLength(0)
  expect(host.textContent).toContain(text('questionPrepared'))
  expect(host.querySelector('img')).toBeNull()
  expect(host.querySelector<HTMLTextAreaElement>('[name="question"]')!.value).toBe('')
  await click('aiReview')
  const dialog = document.querySelector('[role="dialog"]')!
  expect(dialog.textContent).toContain(question)
  expect(dialog.textContent).toContain('Personal Chat')
  expect(dialog.textContent).toContain(text('aiQuestionBoundary'))
  expect(dialog.textContent).toContain(text('aiConfirmDescription'))
  expect(posts()).toHaveLength(0)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data)).toEqual({
    report: 'tokens_by_model',
    model_id: first,
    question,
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    timezone: 'UTC',
    language: 'en',
  })
  expect(
    requests.every((request) =>
      [
        '/auth/session',
        '/auth/permissions',
        '/insights/reports',
        '/admin/insights/models',
        '/admin/insights/analyze',
      ].includes(request.url!),
    ),
  ).toBe(true)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(
    cache
      .getQueryCache()
      .getAll()
      .some((q) => JSON.stringify(q.state.data)?.includes('SELECT secrets')),
  ).toBe(false)
  expect(Object.values(localStorage).some((value) => String(value).includes(question))).toBe(false)
})

it('retains exact question across language changes while keeping reviewed request language fixed', async () => {
  await mount()
  const question = '  为什么 M1 高？\n请说明  '
  await questionInput(question)
  await click('send')
  await click('aiReview')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(question)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data)).toMatchObject({ question, language: 'en' })
  expect(posts()).toHaveLength(1)
})

it('invalidates question review when editing away and back without silently confirming an old draft', async () => {
  await mount()
  await questionInput('Explain the aggregate')
  await click('send')
  await click('aiReview')
  const oldConfirm = button(text('aiConfirm'))
  await questionInput('Different unsubmitted question')
  await questionInput('')
  await act(async () => oldConfirm.click())
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(posts()).toHaveLength(0)
  await launch()
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data).question).toBe('Explain the aggregate')
})

it('aborts a pending question and rejects its late answer even when the same question is submitted again', async () => {
  await mount()
  await questionInput('First question')
  await click('send')
  aiGate = deferred()
  await launch()
  const gate = aiGate
  const signal = posts()[0].signal!
  await questionInput('First question')
  await click('send')
  expect(signal.aborted).toBe(true)
  await act(async () => gate.release())
  expect(host.textContent).not.toContain('M1 advisory')
  expect(posts()).toHaveLength(1)
})

it.each([
  ['界'.repeat(342), 'questionTooLong'],
  ['a\u007fb', 'questionInvalid'],
  ['a\u0085b', 'questionInvalid'],
  ['a\ud800b', 'questionInvalid'],
])('rejects invalid question before preparing a model request', async (question, key) => {
  await mount()
  await questionInput(question)
  await click('send')
  expect(host.textContent).toContain(text(key))
  expect(posts()).toHaveLength(0)
})

it('checks the encoded envelope before confirmation without changing valid question bytes', async () => {
  await mount()
  await questionInput('"'.repeat(1024))
  await click('send')
  await click('aiReview')
  expect(host.textContent).toContain(text('questionEnvelopeTooLarge'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(posts()).toHaveLength(0)
})

it('keeps the reviewed language fixed when UI language changes before confirmation', async () => {
  await mount()
  await click('aiReview')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain(
    i18n.t('insights:aiLanguage', { language: text('aiEnglish') }),
  )
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data).language).toBe('en')
  expect(posts()).toHaveLength(1)
})
it('hides a completed answer during renewed model reads and cannot restore it from identical options', async () => {
  await mount()
  await launch()
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  modelGate = deferred()
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['insights-models'] })
  })
  expect(host.textContent).not.toContain('M1 advisory')
  await act(async () => modelGate!.release())
  await until(() => expect(host.textContent).toContain('Personal Chat'))
  expect(host.textContent).not.toContain('M1 advisory')
  expect(posts()).toHaveLength(1)
})
it('changes the CSRF authority lifetime before a prepared confirmation can dispatch', async () => {
  await mount()
  await click('aiReview')
  csrf = 'rotated-csrf'
  await act(async () => {
    cache.setQueryData(sessionKey, session())
  })
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(posts()).toHaveLength(0)
})

it('formats confirmation and answer dates in the selected language and captured timezone without changing wire timestamps', async () => {
  await mount()
  await input('timezone', 'Asia/Shanghai')
  await click('aiReview')
  const visibleRange = (locale: string) =>
    i18n.t('insights:range', {
      from: usageTime('2026-09-01T00:00:00Z', locale, 'Asia/Shanghai'),
      to: usageTime('2026-09-02T00:00:00Z', locale, 'Asia/Shanghai'),
      timezone: 'Asia/Shanghai',
    })
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(visibleRange('en-US'))
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(visibleRange('zh-CN'))
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(JSON.parse(posts()[0].data)).toMatchObject({
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    timezone: 'Asia/Shanghai',
    language: 'en',
  })
  expect(host.textContent).toContain(visibleRange('zh-CN'))
  expect(host.textContent).toContain(
    i18n.t('insights:queriedAt', {
      time: usageTime('2026-09-03T00:00:00Z', 'zh-CN', 'Asia/Shanghai'),
    }),
  )
  expect(host.textContent).toContain(
    i18n.t('insights:latestCompletedAt', {
      time: usageTime('2026-09-02T23:00:00Z', 'zh-CN', 'Asia/Shanghai'),
    }),
  )
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  expect(host.textContent).toContain(visibleRange('en-US'))
  expect(host.textContent).toContain(
    i18n.t('insights:queriedAt', {
      time: usageTime('2026-09-03T00:00:00Z', 'en-US', 'Asia/Shanghai'),
    }),
  )
  expect(posts()).toHaveLength(1)
})

it('keeps a failed question transient and requires another reviewed confirmation instead of replaying inference', async () => {
  await mount()
  const question = '  Explain known aggregate usage only  '
  await questionInput(question)
  await click('send')
  errorStatus = 502
  await launch()
  await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
  expect(host.textContent).not.toContain('private-provider-secret')
  expect(posts()).toHaveLength(1)
  errorStatus = 0
  await click('aiReview')
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(question)
  expect(posts()).toHaveLength(1)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('M1 advisory'))
  expect(posts()).toHaveLength(2)
  expect(JSON.parse(posts()[1].data)).toEqual(JSON.parse(posts()[0].data))
})

it('keeps Provider quality fixed when question prose requests private Team data', async () => {
  providerAllowed = true
  await mount()
  await click('shortcutProvider')
  await questionInput('Please explain Provider errors and private Team Keys')
  const before = requests.length
  await click('send')
  expect(posts()).toHaveLength(0)
  expect(requests).toHaveLength(before)
  expect(host.textContent).toContain(text('questionPrepared'))
  await click('aiReview')
  const dialog = document.querySelector('[role="dialog"]')!
  expect(dialog.textContent).toContain(text('shortcutProvider'))
  expect(dialog.textContent).toContain(text('aiQuestionBoundary'))
  expect(dialog.textContent).toContain('Personal Chat')
  expect(dialog.textContent).toContain(
    i18n.t('insights:range', {
      from: usageTime('2026-09-01T00:00:00Z', 'en-US', 'UTC'),
      to: usageTime('2026-09-02T00:00:00Z', 'en-US', 'UTC'),
      timezone: 'UTC',
    }),
  )
  expect(posts()).toHaveLength(0)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('P1 advisory'))
  expect(JSON.parse(posts()[0].data)).toMatchObject({
    report: 'provider_quality',
    question: 'Please explain Provider errors and private Team Keys',
  })
})
