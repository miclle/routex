import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { ProviderModel } from '@/types/catalog'
import ProviderModelState from './provider-model-state'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let container: HTMLDivElement, root: Root, cache: QueryClient, model: ProviderModel
let permissions: string[], writes: InternalAxiosRequestConfig[], failure: number
const originalAdapter = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  model = { id: 'pmd_state', upstream_name: 'text-model', enabled: true, etag: '0' }
  permissions = ['providers.read', 'providers.write']
  writes = []
  failure = 0
  client.defaults.adapter = async (config) => {
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = { user: { id: 'usr_state', role: 'member' }, csrf_token: 'csrf-state' }
    if (config.url === '/auth/permissions') response.data = { permissions }
    if (config.method === 'patch') {
      writes.push(config)
      if (failure) {
        if (failure === 503) model = { ...model, enabled: false, etag: 'uncertain' }
        response.status = failure
        throw new AxiosError('State conflict', '', config, undefined, response)
      }
      model = { ...model, enabled: JSON.parse(config.data).enabled, etag: 'saved' }
      response.data = model
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
function Host() {
  const [current, setCurrent] = useState(model)
  return (
    <ProviderModelState
      model={current}
      reload={async () => {
        setCurrent({ ...model })
        return { ...model }
      }}
    />
  )
}
async function until(check: () => void) {
  for (let index = 0; index < 60; index++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (index === 59) throw error
    }
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Host />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(container.textContent).toContain('Provider-side availability'))
}
function button(text: string) {
  const result = [...container.querySelectorAll('button')].find((item) => item.textContent === text)
  if (!result) throw new Error(`Missing button: ${text}`)
  return result
}
async function disable() {
  await until(() => expect(container.querySelector('[role="switch"]')).not.toBeNull())
  await act(async () => (container.querySelector('[role="switch"]') as HTMLButtonElement).click())
}
describe('provider model availability', () => {
  it('submits exact reviewed state, ETag and CSRF once', async () => {
    await render()
    await disable()
    await act(async () => {
      const form = container.querySelector('form')!
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => expect(container.textContent).toContain('Availability saved and published.'))
    expect(writes).toHaveLength(1)
    expect(JSON.parse(writes[0].data)).toEqual({ etag: '0', enabled: false })
    expect(writes[0].headers.get('X-CSRF-Token')).toBe('csrf-state')
    expect(button('Save status').disabled).toBe(true)
  })
  it('requires a fresh review after conflict and retains the selection', async () => {
    failure = 409
    await render()
    await disable()
    await act(async () => button('Save status').click())
    await until(() => expect(button('Save status').disabled).toBe(true))
    model = { ...model, etag: 'current' }
    failure = 0
    await act(async () => button('Reload and review').click())
    await until(() => expect(button('Save status').disabled).toBe(false))
    await act(async () => button('Save status').click())
    expect(JSON.parse(writes[1].data)).toEqual({ etag: 'current', enabled: false })
  })
  it('reconciles an uncertain saved change without blindly resubmitting', async () => {
    failure = 503
    await render()
    await disable()
    await act(async () => button('Save status').click())
    await until(() => expect(container.textContent).toContain('Publication could not be confirmed'))
    await act(async () => button('Reload and review').click())
    expect(button('Save status').disabled).toBe(false)
    expect(writes).toHaveLength(1)
    failure = 0
    await act(async () => button('Save status').click())
    expect(JSON.parse(writes[1].data)).toEqual({ etag: 'uncertain', enabled: false })
    expect(button('Save status').disabled).toBe(true)
  })
  it('shows availability without editing for read-only users', async () => {
    permissions = ['providers.read']
    await render()
    expect(container.querySelector('[role="switch"]')).toBeNull()
    expect(container.textContent).toContain('Enabled')
  })
  it('updates labels when switching to Chinese', async () => {
    await render()
    await disable()
    await act(async () => i18n.changeLanguage('zh'))
    expect(container.textContent).toContain('供应商侧启用状态')
    expect(button('保存状态')).toBeTruthy()
  })
})
