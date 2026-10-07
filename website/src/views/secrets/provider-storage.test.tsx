import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import ProviderStoragePolicyEditor from './provider-storage'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const id = 'vlt_01k0000000000000000000000a',
  revision = 'vlr_01k0000000000000000000000b'
const permissionsKey = ['permissions', 'usr_admin', 'secrets', 0]
let root: Root, host: HTMLDivElement, cache: QueryClient
let policy: {
  mode: 'inline' | 'vault'
  integration_id: string | null
  revision_id: string | null
  choices: { id: string; revision_id: string; name: string; birth: string }[]
  etag: string
  can_edit: boolean
}
let requests: InternalAxiosRequestConfig[], status: number, held: Promise<void> | undefined
const original = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: 'usr_admin', role: 'admin' },
    csrf_token: 'a'.repeat(64),
  })
  cache.setQueryData(permissionsKey, ['secrets.read', 'secrets.write'])
  policy = {
    mode: 'inline',
    integration_id: null,
    revision_id: null,
    choices: [{ id, revision_id: revision, name: 'Saved Vault', birth: '2026-09-23T00:00:00Z' }],
    etag: 'b'.repeat(64),
    can_edit: true,
  }
  requests = []
  status = 200
  held = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (config.method === 'put') {
      const captured = status
      if (held) await held
      if (captured !== 200)
        throw new AxiosError('Safe failure', '', config, undefined, {
          data: {},
          status: captured,
          statusText: '',
          config,
          headers: {},
        })
      const body = JSON.parse(config.data)
      Object.assign(
        policy,
        { mode: body.mode, integration_id: body.integration_id, revision_id: body.revision_id },
        { etag: 'c'.repeat(64) },
      )
    }
    return {
      data: structuredClone(policy),
      status: 200,
      statusText: '',
      config,
      headers: { etag: `"${policy.etag}"` },
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((done) => setTimeout(done, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === text,
  )!
async function click(text: string) {
  await act(async () => button(text).click())
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ProviderStoragePolicyEditor
          actor="usr_admin"
          generation={0}
          permissionKey={permissionsKey}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Configured source'))
}
async function draft() {
  await click('Switch mode')
  await act(async () => {
    const select = document.querySelectorAll<HTMLSelectElement>('[role="dialog"] select')[1]
    select.value = `${id}:${revision}`
    select.dispatchEvent(new Event('change', { bubbles: true }))
    const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'Future Vault writes',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const writes = () => requests.filter((item) => item.method === 'put')
it('renders two stacked source cards and requires exact saved revision, reason and explicit confirmation', async () => {
  await mount()
  expect(host.querySelectorAll('h2')).toHaveLength(2)
  expect(host.textContent).not.toMatch(/healthy|available now|enabled integration/i)
  await draft()
  await click('Continue')
  expect(writes()).toHaveLength(0)
  await click('Confirm policy change')
  await until(() => expect(document.body.textContent).toContain('Future-write policy saved'))
  expect(JSON.parse(writes()[0].data)).toEqual({
    mode: 'vault',
    integration_id: id,
    revision_id: revision,
    reason: 'Future Vault writes',
  })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
})
it('does not synthesize eligibility from configured IDs and keeps independent write denial', async () => {
  policy.mode = 'vault'
  policy.integration_id = id
  policy.revision_id = revision
  policy.choices = []
  await mount()
  const configure = button('Configure')
  await act(async () => configure.click())
  expect(document.body.textContent).toContain('Saved policy IDs alone')
  expect(button('Continue').disabled).toBe(true)
  await act(async () => cache.setQueryData(permissionsKey, ['secrets.read']))
  expect(button('Continue').disabled).toBe(true)
  expect(writes()).toHaveLength(0)
})
it('retains draft and requires explicit review after fresh policy changes', async () => {
  await mount()
  await draft()
  policy.etag = 'd'.repeat(64)
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'provider-storage-policy'] })
  })
  expect(button('Continue').disabled).toBe(true)
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.value).toBe(
    'Future Vault writes',
  )
  await click('Review current policy')
  await click('Continue')
  await click('Confirm policy change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(writes()[0].headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
})
it('503 and rejected retry preserve the original body and reviewed ETag while using new CSRF', async () => {
  await mount()
  await draft()
  await click('Continue')
  status = 503
  await click('Confirm policy change')
  await until(() => expect(button('Retry original policy request')).toBeTruthy())
  const body = writes()[0].data
  policy.etag = 'd'.repeat(64)
  await act(async () => {
    cache.setQueryData(sessionKey, {
      user: { id: 'usr_admin', role: 'admin' },
      csrf_token: 'e'.repeat(64),
    })
    await cache.refetchQueries({ queryKey: ['admin', 'provider-storage-policy'] })
  })
  status = 409
  await click('Retry original policy request')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(body)
  expect(writes()[1].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('e'.repeat(64))
  expect(button('Retry original policy request')).toBeTruthy()
})
it('late200 after authority withdrawal stays uncertain and cannot dispatch while denied', async () => {
  await mount()
  await draft()
  await click('Continue')
  let release!: () => void
  held = new Promise((resolve) => {
    release = resolve
  })
  await click('Confirm policy change')
  await act(async () => cache.setQueryData(permissionsKey, ['secrets.read']))
  await act(async () => release())
  await until(() => expect(document.body.textContent).toContain('result is uncertain'))
  expect(button('Retry original policy request').disabled).toBe(true)
})
it('switches visible copy live without discarding the reason and supports dialog Escape', async () => {
  await mount()
  await draft()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('后续写入的存储方式')
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')!.value).toBe(
    'Future Vault writes',
  )
  await act(async () =>
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
})
it('secrets.read alone shows configured facts but cannot open a write; write alone fetches no inventory', async () => {
  cache.setQueryData(permissionsKey, ['secrets.read'])
  await mount()
  expect(button('Switch mode').disabled).toBe(true)
  await act(async () => cache.setQueryData(permissionsKey, ['secrets.write']))
  expect(host.querySelector('h2')).toBeNull()
  expect(writes()).toHaveLength(0)
})
