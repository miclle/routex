import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import * as api from '@/api/vault-integrations'
import { VaultDrawer, type VaultAuthority } from './vault-drawer'
import {
  certificatePEM,
  privateKeyPEM,
  certificateReplacement,
  certificateVault,
  vault,
  vaultETag,
  vaultID,
} from './vault-fixtures'
import type { VaultConfigIntent } from '@/types/vault-integrations'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  gate: VaultAuthority | null,
  visible: boolean,
  closed: boolean,
  reviewer: () => Promise<string | undefined>,
  saved: ReturnType<
    typeof vi.fn<(result: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void>
  >
async function render(edit = true, integration = vault()) {
  await act(async () =>
    root.render(
      <VaultDrawer
        open={!closed}
        visible={visible}
        id={edit ? vaultID : undefined}
        integration={edit ? integration : undefined}
        etag={vaultETag}
        canSave
        authority={() => gate}
        close={() => {
          closed = true
          void render(edit)
        }}
        review={reviewer}
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
    .querySelector('input, textarea')!
  await act(async () => {
    const prototype =
      node instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(node, value)
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
  reviewer = async () => vaultETag
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

it('saved AppRole writer replacement captures one complete tuple and keeps the reader independent', async () => {
  vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await render()
  const writer = document.querySelector('[role=group][aria-label="Write identity"]')!
  const press = async (label: string) =>
    act(async () =>
      [...writer.querySelectorAll<HTMLButtonElement>('button')]
        .find((x) => x.textContent === label)!
        .click(),
    )
  await press('Replace Token')
  await press('AppRole')
  await input('Writer authentication mount', 'custom/team')
  await input('Writer Role ID', 'writer-role')
  await input('Writer Secret ID', 'writer-secret-id')
  await input('Reason', 'Reviewed identities')
  await click('Save configuration')
  await click('Confirm action')
  expect(api.saveVaultIntegration).toHaveBeenCalledOnce()
  expect(vi.mocked(api.saveVaultIntegration).mock.calls[0][0].input).toMatchObject({
    writer_auth: {
      action: 'replace',
      method: 'approle',
      auth_mount: 'custom/team',
      role_id: 'writer-role',
      secret_id: 'writer-secret-id',
    },
    reader_auth: { action: 'keep' },
  })
})

async function identityPress(kind: 'writer' | 'reader', label: string) {
  const card = document.querySelector(
    `[role=group][aria-label="${kind === 'writer' ? 'Write identity' : 'Read identity'}"]`,
  )!
  await act(async () =>
    [...card.querySelectorAll<HTMLButtonElement>('button')]
      .find((node) => node.textContent === label)!
      .click(),
  )
}
async function writerAppRole() {
  await identityPress('writer', 'Replace Token')
  await identityPress('writer', 'AppRole')
  await input('Writer authentication mount', 'custom/team')
  await input('Writer Role ID', 'writer-role')
  await input('Writer Secret ID', 'writer-secret-id')
}
it('method switches clear obsolete replacement material and require a fresh complete tuple', async () => {
  await render()
  await writerAppRole()
  await input('Reason', 'Review')
  expect(button('Save configuration').disabled).toBe(false)
  await identityPress('writer', 'Token')
  expect(document.querySelector<HTMLInputElement>('input[type=password]')!.value).toBe('')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Writer Token', 'new-token')
  await identityPress('writer', 'AppRole')
  const values = [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value)
  expect(values).not.toContain('new-token')
  expect(values).not.toContain('writer-secret-id')
  expect(button('Save configuration').disabled).toBe(true)
  await identityPress('writer', 'Keep saved Token')
  expect(document.querySelector('input[type=password]')).toBeNull()
})
it('language switching preserves the exact AppRole replacement without submitting', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration')
  await render()
  await writerAppRole()
  await input('Reason', 'human reason')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('写入身份')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value),
  ).toEqual(
    expect.arrayContaining(['custom/team', 'writer-role', 'writer-secret-id', 'human reason']),
  )
  expect(spy).not.toHaveBeenCalled()
})
it('AppRole uncertain retries retain the complete tuple, reason, UUID and review through rejected retries', async () => {
  const requests: VaultConfigIntent[] = [],
    csrf: string[] = []
  vi.spyOn(api, 'saveVaultIntegration').mockImplementation(async (intent, token) => {
    requests.push(structuredClone(intent))
    csrf.push(token)
    throw new api.VaultError(requests.length === 1 ? 503 : 409)
  })
  await render()
  await writerAppRole()
  await input('Reason', 'Review tuple')
  await click('Save configuration')
  await click('Confirm action')
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  await render()
  expect(requests).toHaveLength(1)
  await click('Retry original intent')
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
  expect(requests[0].input.writer_auth).toEqual({
    action: 'replace',
    method: 'approle',
    auth_mount: 'custom/team',
    role_id: 'writer-role',
    secret_id: 'writer-secret-id',
  })
  expect(csrf).toEqual(['c'.repeat(64), 'd'.repeat(64)])
  expect(button('Retry original intent')).toBeDefined()
  expect(saved).not.toHaveBeenCalled()
})
it('held AppRole success after renewal stays uncertain and hidden until fresh authority returns', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void
  vi.spyOn(api, 'saveVaultIntegration').mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  await render()
  await writerAppRole()
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  visible = false
  gate = null
  await render()
  expect(document.querySelector('input[type=password]')).toBeNull()
  await act(async () =>
    resolve({
      request_id: vi.mocked(api.saveVaultIntegration).mock.calls[0][0].input.request_id,
      integration_id: vaultID,
      revision_id: vault().revision_id,
      committed: true,
      changed: true,
    }),
  )
  expect(saved).not.toHaveBeenCalled()
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  visible = true
  await render()
  expect(document.body.textContent).toContain('outcome is unknown')
  expect(button('Retry original intent')).toBeDefined()
})
it('AppRole dismissal destroys tuple material on remount', async () => {
  await render()
  await writerAppRole()
  await click('Cancel')
  await act(async () => root.unmount())
  root = createRoot(host)
  closed = false
  await render()
  expect(document.querySelector('input[type=password]')).toBeNull()
  await identityPress('writer', 'Replace Token')
  await identityPress('writer', 'AppRole')
  const values = [...document.querySelectorAll<HTMLInputElement>('input')].map((node) => node.value)
  expect(values).not.toContain('writer-secret-id')
  expect(values).not.toContain('writer-role')
})
it('reader AppRole replacement remains independent from a kept Token writer', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await render()
  await identityPress('reader', 'Replace Token')
  await identityPress('reader', 'AppRole')
  await input('Reader authentication mount', 'auth/team')
  await input('Reader Role ID', 'reader-role')
  await input('Reader Secret ID', 'reader-secret')
  await input('Reason', 'Reader review')
  await click('Save configuration')
  await click('Confirm action')
  expect(spy.mock.calls[0][0].input).toMatchObject({
    writer_auth: { action: 'keep' },
    reader_auth: {
      action: 'replace',
      method: 'approle',
      auth_mount: 'auth/team',
      role_id: 'reader-role',
      secret_id: 'reader-secret',
    },
  })
})
it('creation blocks identical complete AppRole replacements while accepting independently different material', async () => {
  await render(false)
  await input('Integration name', 'Vault')
  await input('Vault address', 'https://vault.test')
  await input('Reason', 'Create')
  for (const kind of ['writer', 'reader'] as const) {
    await identityPress(kind, 'AppRole')
    const title = kind === 'writer' ? 'Writer' : 'Reader'
    await input(`${title} Role ID`, 'same-role')
    await input(`${title} Secret ID`, 'same-secret')
  }
  expect(button('Save configuration').disabled).toBe(true)
  await input('Reader Secret ID', 'different-secret')
  expect(button('Save configuration').disabled).toBe(false)
})

async function identityCertificate(kind: 'writer' | 'reader', create = false) {
  if (!create) await identityPress(kind, 'Replace Token')
  await identityPress(kind, 'TLS certificate')
  const title = kind === 'writer' ? 'Writer' : 'Reader'
  await input(title + ' authentication mount', 'cert/team')
  await input(title + ' certificate role', kind + '-role')
  await input(title + ' certificate chain (PEM)', certificatePEM)
  await input(title + ' private key (PEM)', privateKeyPEM)
}
it('offers independent certificate identities with explicit mount, role and transient PEM fields', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await render(false)
  await input('Integration name', 'Vault')
  await input('Vault address', 'https://vault.test')
  await input('Reason', 'Review certificate identities')
  await identityCertificate('writer', true)
  expect(button('Save configuration').disabled).toBe(true)
  await identityCertificate('reader', true)
  expect(button('Save configuration').disabled).toBe(false)
  await input('Reader certificate role', 'writer-role')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Reader certificate role', 'reader-role')
  await click('Save configuration')
  expect(spy).not.toHaveBeenCalled()
  await click('Confirm action')
  expect(spy).toHaveBeenCalledOnce()
  expect(spy.mock.calls[0][0].input).toMatchObject({
    writer_auth: certificateReplacement(),
    reader_auth: { ...certificateReplacement(), certificate_role: 'reader-role' },
  })
})
it('certificate drafts require HTTPS and a complete named identity before review', async () => {
  await render()
  await identityCertificate('writer')
  await input('Reason', 'Review')
  await input('Vault address', 'http://vault.test')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Vault address', 'https://vault.test')
  expect(button('Save configuration').disabled).toBe(false)
  await input('Writer certificate role', '')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Writer certificate role', 'writer-role')
  await input('Writer private key (PEM)', '')
  expect(button('Save configuration').disabled).toBe(true)
})
it('switching certificate method or auth action clears all obsolete PEM and role drafts', async () => {
  await render()
  await identityCertificate('writer')
  await input('Writer server CA (PEM, optional)', certificatePEM)
  await identityPress('writer', 'Token')
  expect(document.querySelector('textarea')).toBeNull()
  await identityPress('writer', 'TLS certificate')
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual(['', '', ''])
  await input('Writer certificate chain (PEM)', certificatePEM)
  await identityPress('writer', 'Remove Token')
  expect(document.querySelector('textarea')).toBeNull()
  await identityPress('writer', 'Replace certificate')
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual(['', '', ''])
})
it('certificate unknown intent survives rejected retry and language switch with exact original PEM bytes', async () => {
  const spy = vi
    .spyOn(api, 'saveVaultIntegration')
    .mockRejectedValueOnce(new api.VaultError(503))
    .mockRejectedValueOnce(new api.VaultError(409))
  await render()
  await identityCertificate('writer')
  await input('Writer server CA (PEM, optional)', certificatePEM)
  await input('Reason', 'Review original identity')
  await click('Save configuration')
  await click('Confirm action')
  const original = structuredClone(spy.mock.calls[0][0])
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('写入证书链（PEM）')
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual([
    certificatePEM,
    privateKeyPEM,
    certificatePEM,
  ])
  expect([...document.querySelectorAll('textarea')].every((node) => node.disabled)).toBe(true)
  expect(spy).toHaveBeenCalledOnce()
  await click('重试原始意图')
  expect(spy.mock.calls[1][0]).toEqual(original)
  expect(spy.mock.calls[1][1]).toBe('d'.repeat(64))
  expect(saved).not.toHaveBeenCalled()
  expect(button('重试原始意图')).toBeDefined()
})
it('keeps saved certificate metadata secret-free and requires explicit replacement or removal', async () => {
  await render(true, certificateVault())
  expect(document.querySelector('textarea')).toBeNull()
  await input('Reason', 'Review')
  await input('Vault address', 'http://vault.test')
  expect(button('Save configuration').disabled).toBe(true)
  await input('Vault address', 'https://vault.test')
  expect(
    [...document.querySelectorAll('button[aria-pressed=true]')].map((node) => node.textContent),
  ).toEqual(['Keep saved certificate', 'Keep saved certificate'])
  await identityPress('writer', 'Replace certificate')
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual(['', '', ''])
  await identityPress('writer', 'Remove certificate')
  expect(document.querySelector('textarea')).toBeNull()
})
it('dismissed certificate drafts cannot return after remount', async () => {
  await render()
  await identityCertificate('writer')
  await click('Cancel')
  await act(async () => root.unmount())
  root = createRoot(host)
  closed = false
  await render()
  await identityPress('writer', 'Replace Token')
  await identityPress('writer', 'TLS certificate')
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual(['', '', ''])
})

it('successful certificate save clears all PEM drafts and retains only the safe receipt', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockImplementation(async (intent) => ({
    request_id: intent.input.request_id,
    integration_id: vaultID,
    revision_id: vault().revision_id,
    committed: true,
    changed: true,
  }))
  await render()
  await identityCertificate('writer')
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  expect(spy).toHaveBeenCalledOnce()
  expect(saved).toHaveBeenCalledOnce()
  expect([...document.querySelectorAll('textarea')].map((node) => node.value)).toEqual(['', '', ''])
  expect(document.body.textContent).not.toContain('YQ==')
  expect(document.body.textContent).not.toContain('Yg==')
})
it('late certificate completion after renewal stays unknown and never signals saved', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof api.saveVaultIntegration>>) => void
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  await render()
  await identityCertificate('reader')
  await input('Reason', 'Review')
  await click('Save configuration')
  await click('Confirm action')
  const original = structuredClone(spy.mock.calls[0][0])
  visible = false
  gate = null
  await render()
  expect(document.querySelector('textarea')).toBeNull()
  await act(async () =>
    resolve({
      request_id: original.input.request_id,
      integration_id: vaultID,
      revision_id: vault().revision_id,
      committed: true,
      changed: true,
    }),
  )
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  visible = true
  await render()
  expect(saved).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('outcome is unknown')
  expect(button('Retry original intent')).toBeDefined()
  expect(spy).toHaveBeenCalledOnce()
})

// Keep the actual rendered callback: replacing a DOM listener is not a dispatch fence.
function retainButtonCallback(node: HTMLButtonElement) {
  const key = Object.getOwnPropertyNames(node).find((name) => name.startsWith('__reactProps$'))!
  expect(key).toBeDefined()
  const callback = (node as unknown as Record<string, { onClick: () => void }>)[key].onClick
  expect(callback).toBeTypeOf('function')
  return callback
}
function confirmationCancel() {
  const confirm = button('Confirm action')
  return [...confirm.closest('[role=dialog]')!.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent === 'Cancel',
  )!
}
async function prepareCertificateConfirmation() {
  await render()
  await identityCertificate('writer')
  await input('Reason', 'Original certificate review')
  await click('Save configuration')
}
it('cancel revokes the retained certificate confirmation before any private tuple dispatch', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await prepareCertificateConfirmation()
  const prior = retainButtonCallback(button('Confirm action'))
  await act(async () => confirmationCancel().click())
  await act(async () => prior())
  expect(spy).not.toHaveBeenCalled()
  expect(saved).not.toHaveBeenCalled()
  await input('Reason', 'New explicit certificate review')
  await click('Save configuration')
  await act(async () => prior())
  expect(spy).not.toHaveBeenCalled()
  await click('Confirm action')
  expect(spy).toHaveBeenCalledOnce()
  expect(spy.mock.calls[0][0].input).toMatchObject({
    reason: 'New explicit certificate review',
    writer_auth: certificateReplacement(),
  })
})
it.each(['review', 'method', 'action', 'renewal', 'hidden', 'dismissal', 'unmount'] as const)(
  '%s invalidates a retained certificate confirmation without dispatch',
  async (change) => {
    const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
    await prepareCertificateConfirmation()
    const prior = retainButtonCallback(button('Confirm action'))
    if (change === 'review') await click('Review current status')
    if (change === 'method') await identityPress('writer', 'Token')
    if (change === 'action') await identityPress('writer', 'Keep saved Token')
    if (change === 'renewal') gate = { csrf: 'd'.repeat(64), epoch: 2 }
    if (change === 'hidden') {
      visible = false
      await render()
    }
    if (change === 'dismissal') {
      const outerCancel = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
        (node) => node.textContent === 'Cancel',
      )!
      await act(async () => outerCancel.click())
    }
    if (change === 'unmount') {
      await act(async () => root.unmount())
      root = createRoot(host)
    }
    await act(async () => prior())
    expect(spy).not.toHaveBeenCalled()
    expect(saved).not.toHaveBeenCalled()
  },
)
it('obsolete confirmation Cancel cannot erase an already dispatched uncertain certificate intent', async () => {
  const spy = vi
    .spyOn(api, 'saveVaultIntegration')
    .mockRejectedValueOnce(new api.VaultError(503))
    .mockRejectedValueOnce(new api.VaultError(409))
  await prepareCertificateConfirmation()
  const cancel = retainButtonCallback(confirmationCancel()),
    prior = retainButtonCallback(button('Confirm action'))
  await click('Confirm action')
  const original = structuredClone(spy.mock.calls[0][0])
  await act(async () => {
    cancel()
    prior()
  })
  expect(spy).toHaveBeenCalledOnce()
  expect(button('Retry original intent')).toBeDefined()
  gate = { csrf: 'd'.repeat(64), epoch: 2 }
  await render()
  await click('Retry original intent')
  expect(spy).toHaveBeenCalledTimes(2)
  expect(spy.mock.calls[1][0]).toEqual(original)
  expect(spy.mock.calls[1][1]).toBe('d'.repeat(64))
  expect(original.input.writer_auth).toEqual(certificateReplacement())
  expect(saved).not.toHaveBeenCalled()
  expect(button('Retry original intent')).toBeDefined()
})

it('an explicit renewed status read admits only a new confirmation after its own query epochs', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await prepareCertificateConfirmation()
  const prior = retainButtonCallback(button('Confirm action'))
  let resolve!: (etag: string) => void
  reviewer = () =>
    new Promise((done) => {
      resolve = done
    })
  await render()
  await click('Review current status')
  visible = false
  gate = null
  await render()
  gate = { csrf: 'd'.repeat(64), epoch: 3 }
  visible = true
  await render()
  await act(async () => resolve(vaultETag))
  expect(button('Save configuration').disabled).toBe(false)
  await act(async () => prior())
  expect(spy).not.toHaveBeenCalled()
  await click('Save configuration')
  await click('Confirm action')
  expect(spy).toHaveBeenCalledOnce()
  expect(spy.mock.calls[0][1]).toBe('d'.repeat(64))
})

it('restoring visibility cannot revive an old certificate confirmation or its retained callback', async () => {
  const spy = vi.spyOn(api, 'saveVaultIntegration').mockRejectedValue(new api.VaultError(503))
  await prepareCertificateConfirmation()
  const prior = retainButtonCallback(button('Confirm action'))
  visible = false
  await render()
  visible = true
  await render()
  expect(button('Confirm action')).toBeUndefined()
  await act(async () => prior())
  expect(spy).not.toHaveBeenCalled()
  await click('Save configuration')
  await click('Confirm action')
  expect(spy).toHaveBeenCalledOnce()
  expect(spy.mock.calls[0][0].input.writer_auth).toEqual(certificateReplacement())
})
