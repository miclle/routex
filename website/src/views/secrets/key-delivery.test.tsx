import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { getPermissions } from '@/api/governance'
import { getVaultDeliveryOptions, type VaultDeliveryOption } from '@/api/key-delivery-options'
import type { KeyDeliveryAdminWorkspaceProps } from '@/views/key-delivery-admin'
import { KeyDeliveryWorkspace } from './key-delivery'

vi.mock('@/api/governance', () => ({ getPermissions: vi.fn() }))
vi.mock('@/api/key-delivery-options', async (original) => {
  const actual = await original<typeof import('@/api/key-delivery-options')>()
  return { ...actual, getVaultDeliveryOptions: vi.fn() }
})
vi.mock('@/views/key-delivery-admin', () => ({
  default: (props: KeyDeliveryAdminWorkspaceProps) => (
    <div data-testid="workspace">
      {props.choicesCurrent ? props.vaultChoices.map((choice) => choice.name).join(',') : 'hidden'}
    </div>
  ),
}))
const readPermissions = vi.mocked(getPermissions)
const readOptions = vi.mocked(getVaultDeliveryOptions)
const choice: VaultDeliveryOption = {
  id: 'vlt_one',
  name: 'Private destination',
  endpoint: 'https://vault.example.test',
  namespace: '',
  mount: 'kv',
}
let root: Root, host: HTMLDivElement, cache: QueryClient
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function render(props: Partial<Parameters<typeof KeyDeliveryWorkspace>[0]> = {}) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <KeyDeliveryWorkspace actor="usr_admin" admin fresh generation={1} {...props} />
      </QueryClientProvider>,
    )
  })
  await flush()
  await flush()
}
beforeEach(() => {
  readPermissions.mockReset()
  readOptions.mockReset()
  readPermissions.mockResolvedValue(['secrets.read'])
  readOptions.mockResolvedValue([choice])
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
})
it.each([{ admin: false }, { fresh: false }])(
  'does not read delivery configuration destinations outside fresh administrator context: %j',
  async (props) => {
    await render(props)
    expect(readPermissions).not.toHaveBeenCalled()
    expect(readOptions).not.toHaveBeenCalled()
    expect(host.textContent).not.toContain(choice.name)
  },
)
it('waits for independent read permission instead of borrowing write or test authority', async () => {
  const gate = deferred<string[]>()
  readPermissions.mockReturnValue(gate.promise)
  await render()
  expect(readOptions).not.toHaveBeenCalled()
  await act(async () => gate.resolve(['secrets.write', 'secrets.test']))
  await flush()
  expect(readOptions).not.toHaveBeenCalled()
  expect(host.textContent).not.toContain(choice.name)
})
it('shows only successfully read destination choices and hides them during renewed authority', async () => {
  await render()
  expect(host.textContent).toContain(choice.name)
  const gate = deferred<string[]>()
  readPermissions.mockReturnValue(gate.promise)
  await render({ generation: 2, fresh: false })
  expect(host.textContent).not.toContain(choice.name)
  await render({ generation: 2 })
  expect(host.textContent).not.toContain(choice.name)
  await act(async () => gate.resolve([]))
  await flush()
  expect(host.textContent).not.toContain(choice.name)
})
it('does not expose a late old-actor destination reply to the new workspace', async () => {
  const gate = deferred<VaultDeliveryOption[]>()
  readOptions.mockReturnValueOnce(gate.promise)
  await render()
  const oldSignal = readOptions.mock.calls[0]?.[0]
  readPermissions.mockResolvedValue([])
  await render({ actor: 'usr_other', generation: 2 })
  expect(oldSignal?.aborted).toBe(true)
  await act(async () => gate.resolve([choice]))
  await flush()
  expect(host.textContent).not.toContain(choice.name)
})
