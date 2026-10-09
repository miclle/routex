import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { transportFixture, button, click, fill, until } from './provider-model-transport.fixture'
let f: Awaited<ReturnType<typeof transportFixture>>
beforeEach(async () => {
  f = await transportFixture()
})
afterEach(async () => {
  await f.dispose()
})
async function draft() {
  await click('Edit capacity attestation')
  await fill('Maximum billable input tokens', '128000')
  await fill('Maximum billable output tokens', '16384')
  await fill('Capacity evidence', '  Verified native contract  ')
  await fill('Reason for attestation', '  Explicit transport review  ')
}
async function submit() {
  await click('Save attestation')
  expect(f.writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(f.writes()).toHaveLength(1))
}
describe('ProviderModel generation-bound capacity', () => {
  it('displays unconfigured current-proof absence distinctly from positive recorded limits', async () => {
    await f.mount()
    await until(() =>
      expect(f.host.textContent).toContain('No current configured capacity attestation'),
    )
    await click('Edit capacity attestation')
    expect(
      document.querySelector<HTMLInputElement>('input[aria-label="Maximum billable input tokens"]')!
        .value,
    ).toBe('')
    expect(document.body.textContent).toContain('Reviewed revision: 0')
  })
  it('shows historical configured evidence and raw revision even when current-transport proof is stale', async () => {
    f.state.capacity = {
      ...f.state.capacity,
      configured: true,
      revision: `bnd_${'1'.repeat(26)}`,
      max_input_tokens: 8000,
      max_output_tokens: 2000,
      evidence: 'Old endpoint contract',
      transport_current: false,
    }
    await f.mount()
    await until(() => expect(f.host.textContent).toContain('Old endpoint contract'))
    expect(f.host.textContent).toContain('earlier transport')
    expect(f.host.textContent).toContain(`bnd_${'1'.repeat(26)}`)
    expect(f.host.textContent).not.toContain(f.state.capacity.etag)
  })
  it('confirms a complete attestation with only the four original body fields and strong current review token', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    await submit()
    await until(() => expect(f.host.textContent).toContain('Current capacity attestation saved'))
    expect(JSON.parse(f.writes()[0].data)).toEqual({
      max_input_tokens: 128000,
      max_output_tokens: 16384,
      evidence: 'Verified native contract',
      reason: 'Explicit transport review',
    })
    expect(f.writes()[0].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
    expect(f.writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-original')
    expect(f.host.textContent).not.toContain('saved and published')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.state.model.supports_image_input).toBe(true)
    expect(f.state.model.capabilities_transport_current).toBe(false)
  })
  it('retains its original target against duplicate confirm and captured cancel before React commits the lock', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    await click('Save attestation')
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
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => release())
    await until(() => expect(f.host.textContent).toContain('Current capacity attestation saved'))
  })
  it.each(['0', '-1', '1.5', '1e3', '9007199254740992'])(
    'rejects invalid maximum %s before confirmation/dispatch',
    async (value) => {
      await f.mount()
      await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
      await draft()
      await fill('Maximum billable input tokens', value)
      await click('Save attestation')
      expect(document.body.textContent).toContain('positive safe integers')
      expect(f.writes()).toHaveLength(0)
    },
  )
  it.each(['Capacity evidence', 'Reason for attestation'])(
    'retains original UTF-8/nonempty %s bounds',
    async (label) => {
      await f.mount()
      await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
      await draft()
      for (const value of ['   ', '证'.repeat(667)]) {
        await fill(label, value)
        await click('Save attestation')
        expect(document.body.textContent).toContain('2,000 UTF-8 bytes')
        expect(f.writes()).toHaveLength(0)
      }
    },
  )
  it('preserves existing multiline capacity evidence instead of inventing a control-character restriction', async () => {
    f.state.capacity = {
      ...f.state.capacity,
      configured: true,
      revision: `bnd_${'1'.repeat(26)}`,
      max_input_tokens: 8000,
      max_output_tokens: 2000,
      evidence: 'First line\nSecond line',
    }
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await click('Edit capacity attestation')
    await fill('Reason for attestation', 'Preserve multiline evidence')
    await submit()
    expect(JSON.parse(f.writes()[0].data).evidence).toBe('First line\nSecond line')
  })
  it('preserves the draft after initial409 and requires a fresh read plus explicit review of the new token', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    f.state.failure = 409
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('configuration or transport changed'),
    )
    expect(button('Review current configuration').disabled).toBe(true)
    f.state.capacity = {
      ...f.state.capacity,
      etag: 'c'.repeat(64),
      revision: `bnd_${'1'.repeat(26)}`,
      configured: true,
      max_input_tokens: 64000,
      max_output_tokens: 8000,
      evidence: 'Another operator',
      updated_at: '2026-10-09T03:00:00Z',
    }
    await click('Load current attestation')
    await until(() => expect(button('Review current configuration')).toBeTruthy())
    expect(button('Save attestation').disabled).toBe(true)
    await click('Review current configuration')
    expect(
      document.querySelector<HTMLInputElement>('input[aria-label="Maximum billable input tokens"]')!
        .value,
    ).toBe('128000')
    expect(
      document
        .querySelector<HTMLInputElement>('input[aria-label="Capacity evidence"]')!
        .value.trim(),
    ).toBe('Verified native contract')
    f.state.failure = 0
    await click('Save attestation')
    await click('Confirm change')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(f.writes()[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  })
  it.each([503, -1])(
    'retains the exact unknown intent through dismissal, matchingGET and409 retry after %s',
    async (failure) => {
      await f.mount()
      await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
      await draft()
      f.state.failure = failure
      await submit()
      await until(() => expect(button('Retry exact request').disabled).toBe(false))
      const first = f.writes()[0]
      await click('Cancel')
      await click('Edit capacity attestation')
      expect(
        document.querySelector<HTMLInputElement>('input[aria-label="Capacity evidence"]')!.disabled,
      ).toBe(true)
      f.state.failure = 409
      await click('Retry exact request')
      await until(() => expect(f.writes()).toHaveLength(2))
      await until(() => expect(button('Retry exact request').disabled).toBe(false))
      expect(f.writes()[1].data).toBe(first.data)
      expect(f.writes()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
      await click('Review a separate change')
      await until(() => expect(button('Discard retry and review').disabled).toBe(false))
      expect(f.writes()).toHaveLength(2)
      expect(f.host.textContent).not.toContain('Current capacity attestation saved')
      await click('Discard retry and review')
      expect(document.body.textContent).toContain('original result remains unknown')
      f.state.failure = 0
      await click('Save attestation')
      await click('Confirm change')
      await until(() => expect(f.writes()).toHaveLength(3))
      expect(f.writes()[2].headers.get('If-Match')).toBe(
        `"${failure === 503 ? '4'.repeat(64) : 'b'.repeat(64)}"`,
      )
    },
  )
  it('never releases uncertain intent after a403 retry or a fresh current read', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    f.state.failure = -1
    await submit()
    await until(() => expect(button('Retry exact request').disabled).toBe(false))
    f.state.failure = 403
    await click('Retry exact request')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(button('Save attestation').disabled).toBe(true)
    expect(document.body.textContent).toContain('result is unknown')
    expect(f.host.textContent).not.toContain('Current capacity attestation saved')
  })
  it('a transport-review-only change blocks a captured confirmation even if historical capacity revision is unchanged', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    await click('Save attestation')
    const confirm = button('Confirm change')
    const q = f.cache
      .getQueryCache()
      .getAll()
      .find((query) => query.queryKey[1] === 'provider-model-capacity')!
    await act(async () => {
      f.cache.setQueryData(q.queryKey, { ...f.state.capacity, etag: 'd'.repeat(64) })
      confirm.click()
    })
    expect(f.writes()).toHaveLength(0)
  })
  it.each(['session', 'permission', 'catalogue'])(
    'rejects captured capacity confirm after %s authority invalidation',
    async (scope) => {
      await f.mount()
      await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
      await draft()
      await click('Save attestation')
      const confirm = button('Confirm change')
      await act(async () => {
        void f.cache.invalidateQueries(
          scope === 'session'
            ? { queryKey: sessionKey, exact: true }
            : {
                predicate: (q) =>
                  q.queryKey[0] === (scope === 'permission' ? 'permissions' : 'admin'),
              },
        )
        confirm.click()
      })
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('keeps original unknown capacity across same-owner Session renewal and uses current CSRF for manual retry', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    let release!: () => void
    f.state.hold = new Promise((resolve) => {
      release = resolve
    })
    f.state.ignoreAbort = true
    await submit()
    const first = f.writes()[0]
    f.state.csrf = 'csrf-renewed'
    await f.renew()
    await act(async () => release())
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await click('Edit capacity attestation')
    await until(() => expect(button('Retry exact request').disabled).toBe(false))
    expect(f.host.textContent).not.toContain('Current capacity attestation saved')
    f.state.hold = undefined
    f.state.failure = 409
    await click('Retry exact request')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(f.writes()[1].data).toBe(first.data)
    expect(f.writes()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(f.writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  })
  it.each(['actor', 'provider', 'target', 'unmount'])(
    'discards late capacity response after %s change without recreating old private queries',
    async (change) => {
      await f.mount()
      await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
      await draft()
      let release!: () => void
      f.state.hold = new Promise((resolve) => {
        release = resolve
      })
      f.state.ignoreAbort = true
      await submit()
      const capacityQueries = () =>
        f.cache
          .getQueryCache()
          .getAll()
          .filter((q) => q.queryKey[1] === 'provider-model-capacity')
      const obsoleteKeys = capacityQueries().map((q) => q.queryKey)
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
        if (change === 'actor') expect(button('Edit capacity attestation')).toBeTruthy()
        else
          expect(f.host.textContent).toContain(
            change === 'unmount' ? 'Other route' : 'The provider model is unavailable.',
          )
        for (const queryKey of obsoleteKeys)
          expect(f.cache.getQueryCache().find({ queryKey, exact: true })).toBeUndefined()
        expect(capacityQueries().every((q) => q.getObserversCount() > 0)).toBe(true)
      })
      const before = f.cache
        .getQueryCache()
        .getAll()
        .filter((q) => q.queryKey[1] === 'provider-model-capacity')
        .map((q) => JSON.stringify(q.queryKey))
      await act(async () => release())
      expect(f.writes()).toHaveLength(1)
      expect(document.body.textContent).not.toContain('Current capacity attestation saved')
      for (const queryKey of obsoleteKeys)
        expect(f.cache.getQueryCache().find({ queryKey, exact: true })).toBeUndefined()
      expect(
        f.cache
          .getQueryCache()
          .getAll()
          .filter((q) => q.queryKey[1] === 'provider-model-capacity')
          .map((q) => JSON.stringify(q.queryKey)),
      ).toEqual(before)
    },
  )
  it('shows saved stale capacity with independent read-only authority', async () => {
    f.state.permissions = ['providers.read']
    f.state.capacity = {
      ...f.state.capacity,
      configured: true,
      revision: `bnd_${'1'.repeat(26)}`,
      max_input_tokens: 8000,
      max_output_tokens: 2000,
      evidence: 'Retained proof',
    }
    await f.mount()
    await until(() => expect(f.host.textContent).toContain('Retained proof'))
    expect(f.host.textContent).toContain('earlier transport')
    expect(document.body.textContent).not.toContain('Edit capacity attestation')
  })
  it('does not fetch capacity or Providers when write is present without read', async () => {
    f.state.permissions = ['providers.write']
    await f.mount()
    expect(f.state.requests.filter((r) => r.url?.endsWith('/reservation-bound'))).toHaveLength(0)
    expect(f.state.requests.filter((r) => r.url === '/admin/providers')).toHaveLength(0)
    expect(f.writes()).toHaveLength(0)
  })
  it('live language changes preserve drafts and exact unknown retry while translating all new guidance', async () => {
    await f.mount()
    await until(() => expect(button('Edit capacity attestation')).toBeTruthy())
    await draft()
    await act(async () => i18n.changeLanguage('zh'))
    expect(
      document.querySelector<HTMLInputElement>('input[aria-label="容量依据"]')!.value.trim(),
    ).toBe('Verified native contract')
    await act(async () => i18n.changeLanguage('en'))
    f.state.failure = 503
    await submit()
    await until(() => expect(button('Retry exact request').disabled).toBe(false))
    const first = f.writes()[0].data
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('结果未知')
    await act(async () => i18n.changeLanguage('en'))
    f.state.failure = 409
    await click('Retry exact request')
    await until(() => expect(f.writes()).toHaveLength(2))
    expect(f.writes()[1].data).toBe(first)
  })
})
