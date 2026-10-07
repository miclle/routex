import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import * as api from '@/api/vault-integrations'
import { VaultDrawer, type VaultAuthority } from './vault-drawer'
import { vault, vaultETag, vaultID } from './vault-fixtures'
import type { VaultConfigIntent } from '@/types/vault-integrations'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  gate: VaultAuthority | null,
  visible: boolean,
  closed: boolean,
  saved: ReturnType<
    typeof vi.fn<(result: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void>
  >
async function render(edit = true) {
  await act(async () =>
    root.render(
      <VaultDrawer
        open={!closed}
        visible={visible}
        id={edit ? vaultID : undefined}
        integration={edit ? vault() : undefined}
        etag={vaultETag}
        canSave
        authority={() => gate}
        close={() => {
          closed = true
          void render(edit)
        }}
        review={async () => vaultETag}
        saved={saved}
      />,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(label: string, value: string) {
  const node = [...document.querySelectorAll('label')]
    .find((item) => item.textContent?.startsWith(label))!
    .querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
    node.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  gate = { csrf: 'c'.repeat(64), epoch: 1 }
  visible = true
  closed = false
  saved = vi.fn()
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
it('renders 720px approved descriptor and separate identity cards with explicit keep defaults', async () => {
  await render()
  const popup = document.querySelector('[role=dialog]') as HTMLElement
  expect(popup.style.width).toBe('720px')
  expect(document.body.textContent).toContain('Write identity')
  expect(document.body.textContent).toContain('Read identity')
  expect(
    [...document.querySelectorAll('button[aria-pressed=true]')].map((x) => x.textContent),
  ).toEqual(['Keep saved Token', 'Keep saved Token'])
  expect(document.querySelector('input[type=password]')).toBeNull()
})
it('creation has no keep action, requires two different exact Tokens and a reason', async () => {
  await render(false)
  expect(button('Keep saved Token')).toBeUndefined()
  await input('Integration name', 'Vault')
  await input('Vault address', 'https://vault.test')
  await input('Writer Token', 'same')
  await input('Reader Token', 'same')
  await input('Reason', 'Reviewed setup')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Reader Token', 'different')
  expect(button('Save configuration').disabled).toBe(false)
})
it('exact unknown intent retries use fresh CSRF and retain original UUID/body/ETag without auto-submit', async () => {
  const requests: VaultConfigIntent[] = [],
    tokens: string[] = []
  vi.spyOn(api, 'saveVaultIntegration').mockImplementation(async (intent, csrf) => {
    requests.push(structuredClone(intent))
    tokens.push(csrf)
    if (requests.length === 1) throw new api.VaultError(503)
    return {
      request_id: intent.input.request_id,
      integration_id: vaultID,
      revision_id: vault().revision_id,
      committed: true,
      changed: true,
    }
  })
  await render()
  await input('Reason', 'Reviewed configuration')
  await click('Save configuration')
  await click('Confirm action')
  expect(requests).toHaveLength(1)
  expect(document.body.textContent).toContain('outcome is unknown')
  gate = { csrf: 'd'.repeat(64), epoch: 1 }
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(requests).toHaveLength(1)
  await click('重试原始意图')
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
  expect(tokens).toEqual(['c'.repeat(64), 'd'.repeat(64)])
  expect(saved).toHaveBeenCalledTimes(1)
})
it('rejected uncertain retry does not resolve or replace original intent', async () => {
  const spy = vi
    .spyOn(api, 'saveVaultIntegration')
    .mockRejectedValueOnce(new api.VaultError(503))
    .mockRejectedValueOnce(new api.VaultError(409))
  await render()
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  await click('Retry original intent')
  expect(spy).toHaveBeenCalledTimes(2)
  expect(spy.mock.calls[1][0]).toEqual(spy.mock.calls[0][0])
  expect(button('Retry original intent')).toBeDefined()
  expect(saved).not.toHaveBeenCalled()
})
it('fresh initial conflict retains draft and requires explicit review, not success', async () => {
  vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(409))
  await render()
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  expect(button('Save configuration').disabled).toBe(true)
  expect(document.body.textContent).toContain('configuration changed')
  expect(saved).not.toHaveBeenCalled()
  await click('Review current status')
  expect(button('Save configuration').disabled).toBe(false)
})
it('renewal abort and late successful response stays unknown, hides Token fields and never signals saved', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void
  const promise = new Promise<Awaited<ReturnType<typeof api.saveVaultIntegration>>>((done) => {
    resolve = done
  })
  vi.spyOn(api, 'saveVaultIntegration').mockReturnValue(promise)
  await render()
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  visible = false
  gate = null
  await render()
  expect(document.querySelector('[role=dialog]')).toBeNull()
  await act(async () =>
    resolve({
      request_id: '10000000-0000-4000-8000-000000000001',
      integration_id: vaultID,
      revision_id: vault().revision_id,
      committed: true,
      changed: true,
    }),
  )
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  visible = true
  await render()
  expect(document.body.textContent).toContain('outcome is unknown')
  expect(saved).not.toHaveBeenCalled()
})
it('dismissal clears secret inputs through component teardown and reopening never retains Tokens', async () => {
  await render(false)
  await input('Writer Token', 'writer-secret')
  await input('Reader Token', 'reader-secret')
  await click('Cancel')
  await act(async () => root.unmount())
  root = createRoot(host)
  closed = false
  await render(false)
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input[type=password]')].map(
      (node) => node.value,
    ),
  ).toEqual(['', ''])
})
it('live language switching leaves exact Token and reason drafts unchanged', async () => {
  await render(false)
  await input('Writer Token', 'writer-secret')
  await input('Reason', 'human reason')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('写入身份')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('writer-secret')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toContain('human reason')
})
it('immediate authority loss before confirmation prevents dispatch even before the form rerenders', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration')
  await render()
  await input('Reason', 'Review')
  await click('Save configuration')
  gate = null
  await click('Confirm action')
  expect(spy).not.toHaveBeenCalled()
  expect(saved).not.toHaveBeenCalled()
})
it('Escape dismisses the local Base UI drawer without submitting or retaining Tokens after remount', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration')
  await render(false)
  await input('Writer Token', 'writer-secret')
  await act(async () => {
    document
      .querySelector('[role=dialog]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  expect(closed).toBe(true)
  expect(spy).not.toHaveBeenCalled()
  expect(document.querySelector('input[type=password]')).toBeNull()
})
it('unmounted late response cannot invoke saved or retain a mutation cache entry', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void
  vi.spyOn(api, 'saveVaultIntegration').mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  await render()
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  await act(async () => root.unmount())
  root = createRoot(host)
  await act(async () =>
    resolve({
      request_id: '10000000-0000-4000-8000-000000000001',
      integration_id: vaultID,
      revision_id: vault().revision_id,
      committed: true,
      changed: true,
    }),
  )
  expect(saved).not.toHaveBeenCalled()
  expect(document.body.textContent).not.toContain('QA Vault')
})
