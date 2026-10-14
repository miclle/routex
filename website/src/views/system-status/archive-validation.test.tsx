import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import { ArchiveValidation } from './archive-validation'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const id = 'rcv_00000000000000000000000001'
const birth = '2026-10-14T09:01:00Z'
let root: Root, host: HTMLDivElement
let actor: string, authority: string, archiveID: string, archiveBirth: string
let allowed: boolean, hold: boolean, fail: boolean, wrongBirth: boolean, unmounted: boolean
let requests: InternalAxiosRequestConfig[], held: (() => void)[]
function render() {
  root.render(
    <ArchiveValidation
      actorID={actor}
      authority={authority}
      archiveID={archiveID}
      archiveBirth={archiveBirth}
      isCurrent={() => allowed}
    />,
  )
}
async function click() {
  await act(async () => host.querySelector<HTMLButtonElement>('button')!.click())
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_operator'
  authority = 'first'
  archiveID = id
  archiveBirth = birth
  allowed = true
  hold = false
  fail = false
  wrongBirth = false
  unmounted = false
  requests = []
  held = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      data: {
        scope: 'archive_integrity_only',
        archive_id: archiveID,
        archive_created_at: wrongBirth ? '2026-10-14T09:00:00Z' : archiveBirth,
        codec_version: 1,
        observed_at: '2026-10-14T09:02:00Z',
        integrity: 'authenticated',
      },
    }
    const capturedFail = fail
    if (hold) await new Promise<void>((resolve) => held.push(resolve))
    if (capturedFail)
      throw new AxiosError('private ciphertext diagnostic', '', config, undefined, {
        ...response,
        status: 503,
      })
    return response
  }
  await act(async () => render())
})
afterEach(async () => {
  await act(async () => {
    if (!unmounted) root.unmount()
    held.forEach((release) => release())
  })
  host.remove()
  client.defaults.adapter = adapter
})
describe('transient point-in-time integrity review', () => {
  it('runs only explicitly and switches language without another read', async () => {
    expect(requests).toHaveLength(0)
    expect(host.textContent).toContain(en.archiveValidate)
    expect(host.querySelector('button')?.getAttribute('aria-label')).toContain(id)
    await click()
    expect(host.querySelector('[role="status"]')?.textContent).toContain(
      'Encrypted content authenticated',
    )
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain(zh.archiveValidate)
    expect(host.querySelector('[role="status"]')?.textContent).toContain('加密内容')
    expect(requests).toHaveLength(1)
    expect(requests[0].method).toBe('get')
  })
  it('clears prior facts while refreshing and sanitizes failure without automatic retry', async () => {
    await click()
    expect(host.querySelector('[role="status"]')).not.toBeNull()
    hold = true
    fail = true
    await click()
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(host.textContent).toContain(en.archiveValidating)
    await act(async () => held[0]())
    expect(host.querySelector('[role="alert"]')?.textContent).toBe(en.archiveValidationFailed)
    expect(host.textContent).not.toContain('ciphertext')
    expect(requests).toHaveLength(2)
  })
  it('releases old pending immediately on same-actor renewal and isolates late finally from a new read', async () => {
    hold = true
    await click()
    expect(held).toHaveLength(1)
    authority = 'renewed-session-and-permissions'
    await act(async () => render())
    expect(requests[0].signal?.aborted).toBe(true)
    expect(host.querySelector('button')?.disabled).toBe(false)
    await click()
    expect(held).toHaveLength(2)
    await act(async () => held[0]())
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(host.querySelector('button')?.disabled).toBe(true)
    await act(async () => held[1]())
    expect(host.querySelector('[role="status"]')).not.toBeNull()
    expect(host.querySelector('button')?.disabled).toBe(false)
  })
  it.each(['actor', 'identity', 'birth', 'unmount'] as const)(
    'discards held result after %s ownership ends',
    async (change) => {
      hold = true
      await click()
      if (change === 'unmount') {
        await act(async () => root.unmount())
        unmounted = true
      } else {
        if (change === 'actor') actor = 'usr_other'
        if (change === 'identity') archiveID = 'rcv_00000000000000000000000002'
        if (change === 'birth') archiveBirth = '2026-10-14T09:03:00Z'
        await act(async () => render())
      }
      expect(requests[0].signal?.aborted).toBe(true)
      await act(async () => held[0]())
      expect(host.querySelector('[role="status"]')).toBeNull()
      expect(host.querySelector('[role="alert"]')).toBeNull()
    },
  )
  it('does not revive prior facts when a target is left and revisited', async () => {
    await click()
    expect(host.querySelector('[role="status"]')).not.toBeNull()
    archiveID = 'rcv_00000000000000000000000002'
    await act(async () => render())
    archiveID = id
    await act(async () => render())
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(requests).toHaveLength(1)
  })
  it('rejects an exact-ID response with a different reviewed birth', async () => {
    wrongBirth = true
    await click()
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(host.querySelector('[role="alert"]')?.textContent).toBe(en.archiveValidationFailed)
  })
  it('hides facts and blocks dispatch without fresh parent read authority', async () => {
    await click()
    allowed = false
    await act(async () => render())
    expect(host.querySelector('[role="status"]')).toBeNull()
    await click()
    expect(requests).toHaveLength(1)
  })
})
