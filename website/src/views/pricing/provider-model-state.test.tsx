import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { transportFixture, button, click, toggle, until } from './provider-model-transport.fixture'
let f: Awaited<ReturnType<typeof transportFixture>>
beforeEach(async () => {
  f = await transportFixture()
})
afterEach(async () => {
  await f.dispose()
})
async function availability() {
  await toggle('Enable model')
  await click('Save configuration')
  expect(f.writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(f.writes()).toHaveLength(1))
}
describe('ProviderModel current-transport state', () => {
  it('saves availability alone after confirmation without rewriting or reattesting recorded capabilities', async () => {
    await f.mount()
    await availability()
    await until(() =>
      expect(f.host.textContent).toContain('Current ProviderModel configuration saved'),
    )
    expect(JSON.parse(f.writes()[0].data)).toEqual({ etag: '0', enabled: false })
    expect(f.state.model.supports_image_input).toBe(true)
    expect(f.state.model.supports_pdf_input).toBe(false)
    expect(f.state.model.capabilities_transport_current).toBe(false)
    expect(f.writes()[0].headers.get('If-Match')).toBeUndefined()
    expect(f.writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-original')
    expect(f.host.textContent).not.toContain('saved and published')
  })
  it('explicitly reaffirms the unchanged complete pair with current review token and omits availability', async () => {
    await f.mount()
    await click('Review capability declaration')
    expect(document.body.textContent).toContain('even when both values are unchanged')
    expect(f.writes()).toHaveLength(0)
    await click('Confirm change')
    await until(() => expect(f.writes()).toHaveLength(1))
    expect(JSON.parse(f.writes()[0].data)).toEqual({
      etag: '0',
      capability_review_etag: 'a'.repeat(64),
      supports_image_input: true,
      supports_pdf_input: false,
    })
    await until(() => expect(f.host.textContent).toContain('current for this transport'))
  })
  it('requires an explicit combined confirmation before one complete pair and availability save', async () => {
    await f.mount()
    await toggle('Enable model')
    await toggle('Image input')
    await toggle('PDF input')
    await click('Save configuration')
    expect(document.body.textContent).toContain('selected availability change in one request')
    expect(f.writes()).toHaveLength(0)
    await act(async () => {
      button('Confirm change').click()
      button('Confirm change').click()
    })
    await until(() => expect(f.writes()).toHaveLength(1))
    expect(JSON.parse(f.writes()[0].data)).toEqual({
      etag: '0',
      enabled: false,
      capability_review_etag: 'a'.repeat(64),
      supports_image_input: false,
      supports_pdf_input: true,
    })
  })
  it('keeps confirmation and original target against same-turn captured cancel while pending', async () => {
    await f.mount()
    await toggle('Enable model')
    await click('Save configuration')
    let release!: () => void
    f.state.hold = new Promise((resolve) => {
      release = resolve
    })
    const confirm = button('Confirm change'),
      cancel = button('Cancel')
    await act(async () => {
      confirm.click()
      cancel.click()
      confirm.click()
    })
    expect(f.writes()).toHaveLength(1)
    expect(f.host.textContent).toContain('submitted result is unknown')
    await act(async () => release())
    await until(() =>
      expect(f.host.textContent).toContain('Current ProviderModel configuration saved'),
    )
  })
  it('synchronously rejects captured confirmation on Session expiry and reactively hides both private state cards', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await toggle('Enable model')
    await click('Save configuration')
    const confirm = button('Confirm change')
    await act(async () => {
      window.dispatchEvent(new Event('routex:session-expired'))
      confirm.click()
    })
    expect(f.writes()).toHaveLength(0)
    const pricing = i18n.getFixedT('en', 'pricing')
    expect(f.host.querySelector(`section[aria-label="${pricing('supplyState')}"]`)).toBeNull()
    expect(f.host.querySelector(`section[aria-label="${pricing('capacityTitle')}"]`)).toBeNull()
    expect(f.host.textContent).not.toContain('Current ProviderModel configuration saved')
  })
  it('blocks a stale transport review even when raw PM revision and declarations did not change', async () => {
    await f.mount()
    await click('Review capability declaration')
    f.state.model = { ...f.state.model, capability_review_etag: 'c'.repeat(64) }
    await f.refreshCatalogue()
    await until(() => expect(button('Confirm change').disabled).toBe(true))
    await click('Confirm change')
    expect(f.writes()).toHaveLength(0)
    await click('Cancel')
    await click('Review current configuration')
    await click('Review capability declaration')
    await click('Confirm change')
    await until(() => expect(f.writes()).toHaveLength(1))
    expect(JSON.parse(f.writes()[0].data).capability_review_etag).toBe('c'.repeat(64))
  })
  it('preserves the selected draft after a known initial conflict and requires explicit current review', async () => {
    await f.mount()
    f.state.failure = 409
    await toggle('Image input')
    await click('Save configuration')
    await click('Confirm change')
    await until(() =>
      expect(f.host.textContent).toContain('reviewed configuration or transport changed'),
    )
    expect(button('Review current configuration').disabled).toBe(true)
    f.state.model = {
      ...f.state.model,
      etag: `pms_${'1'.repeat(26)}`,
      capability_review_etag: 'd'.repeat(64),
    }
    await f.refreshCatalogue()
    expect(button('Save configuration').disabled).toBe(true)
    await click('Review current configuration')
    f.state.failure = 0
    await click('Save configuration')
    await click('Confirm change')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(JSON.parse(f.writes()[1].data)).toEqual({
      etag: `pms_${'1'.repeat(26)}`,
      capability_review_etag: 'd'.repeat(64),
      supports_image_input: false,
      supports_pdf_input: false,
    })
  })
  it.each([503, -1])(
    'keeps original unknown intent through fresh matching facts and rejected409 retry after %s',
    async (failure) => {
      await f.mount()
      f.state.failure = failure
      await availability()
      await until(() => expect(button('Retry exact request').disabled).toBe(false))
      const first = f.writes()[0].data
      await f.refreshCatalogue()
      expect(f.writes()).toHaveLength(1)
      expect(button('Save configuration').disabled).toBe(true)
      f.state.failure = 409
      await click('Retry exact request')
      await until(() => expect(f.writes()).toHaveLength(2))
      await until(() => expect(button('Retry exact request').disabled).toBe(false))
      expect(f.writes()[1].data).toBe(first)
      expect(f.host.textContent).toContain('historical outcome')
      expect(f.host.textContent).not.toContain('Current ProviderModel configuration saved')
      await click('Review a separate change')
      await until(() => expect(button('Discard retry and review').disabled).toBe(false))
      expect(f.writes()).toHaveLength(2)
      await click('Discard retry and review')
      expect(f.host.textContent).toContain('original result remains unknown')
      expect(f.writes()).toHaveLength(2)
      f.state.failure = 0
      expect(f.state.model.enabled).toBe(failure === -1)
      if (failure === 503) await toggle('Enable model')
      expect(button('Save configuration').disabled).toBe(false)
      await click('Save configuration')
      expect(f.writes()).toHaveLength(2)
      await click('Confirm change')
      await until(() => expect(f.writes()).toHaveLength(3))
      expect(JSON.parse(f.writes()[2].data)).toEqual({
        etag: failure === 503 ? `pms_${'1'.padStart(26, '0')}` : '0',
        enabled: failure === 503,
      })
      expect(f.state.model.supports_image_input).toBe(true)
      expect(f.state.model.supports_pdf_input).toBe(false)
      expect(f.state.model.capabilities_transport_current).toBe(false)
    },
  )
  it.each(['session', 'permission', 'catalogue'])(
    'rejects a rendered confirm captured before %s read invalidation',
    async (scope) => {
      await f.mount()
      await toggle('Enable model')
      await click('Save configuration')
      const captured = button('Confirm change')
      await act(async () => {
        void f.cache.invalidateQueries(
          scope === 'session'
            ? { queryKey: sessionKey, exact: true }
            : {
                predicate: (q) =>
                  q.queryKey[0] === (scope === 'permission' ? 'permissions' : 'admin'),
              },
        )
        captured.click()
      })
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('discards old success on same-owner Session renewal and retries original body with fresh CSRF', async () => {
    await f.mount()
    let release!: () => void
    f.state.hold = new Promise((resolve) => {
      release = resolve
    })
    f.state.ignoreAbort = true
    await availability()
    const original = f.writes()[0].data
    f.state.csrf = 'csrf-renewed'
    await f.renew()
    await act(async () => release())
    await until(() => expect(button('Retry exact request').disabled).toBe(false))
    expect(f.host.textContent).not.toContain('Current ProviderModel configuration saved')
    f.state.hold = undefined
    f.state.failure = 409
    await click('Retry exact request')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(f.writes()[1].data).toBe(original)
    expect(f.writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  })
  it.each(['actor', 'provider', 'target', 'unmount'])(
    'rejects an ignored-abort late response after %s change without private cache resurrection',
    async (change) => {
      await f.mount()
      let release!: () => void
      f.state.hold = new Promise((resolve) => {
        release = resolve
      })
      f.state.ignoreAbort = true
      await availability()
      const adminQueries = () =>
        f.cache
          .getQueryCache()
          .getAll()
          .filter((q) => q.queryKey[0] === 'admin')
      const obsoleteKeys = adminQueries().map((q) => q.queryKey)
      expect(obsoleteKeys.length).toBeGreaterThan(0)
      if (change === 'actor') {
        f.state.actor = 'usr_other'
        await f.renew()
      } else if (change === 'provider')
        await act(async () => {
          await f.router.navigate('/admin/providers/prv_other/models/pmo_one')
        })
      else if (change === 'target')
        await act(async () => {
          await f.router.navigate('/admin/providers/prv_one/models/pmo_other')
        })
      else
        await act(async () => {
          await f.router.navigate('/other')
        })
      await until(() => {
        if (change === 'actor') expect(button('Save configuration')).toBeTruthy()
        else
          expect(f.host.textContent).toContain(
            change === 'unmount' ? 'Other route' : 'The provider model is unavailable.',
          )
        for (const queryKey of obsoleteKeys)
          expect(f.cache.getQueryCache().find({ queryKey, exact: true })).toBeUndefined()
        expect(adminQueries().every((q) => q.getObserversCount() > 0)).toBe(true)
      })
      const before = f.cache
        .getQueryCache()
        .getAll()
        .filter((q) => q.queryKey[0] === 'admin')
        .map((q) => JSON.stringify(q.queryKey))
      await act(async () => release())
      expect(f.writes()).toHaveLength(1)
      expect(document.body.textContent).not.toContain('Current ProviderModel configuration saved')
      for (const queryKey of obsoleteKeys)
        expect(f.cache.getQueryCache().find({ queryKey, exact: true })).toBeUndefined()
      expect(
        f.cache
          .getQueryCache()
          .getAll()
          .filter((q) => q.queryKey[0] === 'admin')
          .map((q) => JSON.stringify(q.queryKey)),
      ).toEqual(before)
    },
  )
  it('renders recorded stale capability facts for read-only authority and does not infer current evidence from enabled state', async () => {
    f.state.permissions = ['providers.read']
    await f.mount()
    expect(f.host.querySelector('[role="switch"]')).toBeNull()
    expect(f.host.textContent).toContain('earlier transport')
    expect(f.host.textContent).toContain('Supported')
  })
  it('does not admit writes when read permission is absent', async () => {
    f.state.permissions = ['providers.write']
    await f.mount()
    expect(f.host.textContent).not.toContain('Provider-side availability')
    expect(f.writes()).toHaveLength(0)
    expect(f.state.requests.filter((r) => r.url === '/admin/providers')).toHaveLength(0)
  })
  it('treats missing current-evidence fields as unknown and prevents implicit capability reattestation', async () => {
    f.state.model = {
      ...f.state.model,
      capabilities_transport_current: undefined,
      capability_review_etag: undefined,
    }
    await f.mount()
    expect(f.host.textContent).toContain('evidence is unknown')
    expect(button('Review capability declaration').disabled).toBe(true)
  })
  it('switches confirmation and unknown guidance live without changing the captured intent', async () => {
    await f.mount()
    await click('Review capability declaration')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('完整图片和 PDF 能力')
    await act(async () => i18n.changeLanguage('en'))
    f.state.failure = 503
    await click('Confirm change')
    await until(() => expect(button('Retry exact request').disabled).toBe(false))
    const body = f.writes()[0].data
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('结果未知')
    await act(async () => i18n.changeLanguage('en'))
    f.state.failure = 409
    await click('Retry exact request')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(f.writes()[1].data).toBe(body)
  })
})
