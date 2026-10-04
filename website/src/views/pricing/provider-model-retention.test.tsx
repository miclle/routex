import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import ProviderModelPage from './provider-model'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  failure: number,
  permissionFailure: number
const original = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  failure = 503
  permissionFailure = 0
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: {
          id: 'usr_retention',
          name: 'Reviewer',
          email: 'dummy@example.test',
          role: 'member',
        },
        csrf_token: 'csrf-retention',
      }
    else if (config.url === '/auth/permissions') {
      if (permissionFailure) {
        response.status = permissionFailure
        throw new AxiosError('permission unavailable', '', config, undefined, response)
      }
      response.data = { permissions: ['providers.read', 'providers.write'] }
    } else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_one',
            name: 'Current provider',
            connections: [
              {
                id: 'con_one',
                name: 'Connection',
                protocol: 'openai_chat',
                base_url: 'https://example.test',
                credentials: [],
                provider_models: [
                  {
                    id: 'pmo_one',
                    upstream_name: 'Model',
                    enabled: true,
                    supports_image_input: false,
                    supports_pdf_input: false,
                    etag: '0',
                  },
                ],
              },
            ],
          },
        ],
      }
    else if (config.url === '/admin/provider-models/pmo_one/reservation-bound') {
      if (config.method === 'put' && failure) {
        response.status = failure
        throw new AxiosError('publication unknown', '', config, undefined, response)
      }
      response.data = {
        provider_model_id: 'pmo_one',
        protocol: 'openai_chat',
        etag: '0',
        configured: false,
        max_input_tokens: 0,
        max_output_tokens: 0,
        evidence: '',
        updated_at: '0001-01-01T00:00:00Z',
      }
    } else if (config.url === '/admin/provider-models/pmo_one' && config.method === 'patch') {
      response.status = failure
      throw new AxiosError('publication unknown', '', config, undefined, response)
    } else throw new Error(`Unexpected request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  await i18n.changeLanguage('en')
})
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await flush()
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/admin/providers/prv_one/models/pmo_one']}>
          <Routes>
            <Route
              path="/admin/providers/:providerId/models/:modelId"
              element={<ProviderModelPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Provider-side availability'))
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find(
    (button) => button.textContent === label,
  )
  expect(result).toBeTruthy()
  return result!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(label: string, value: string) {
  const control = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  expect(control).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function loseAndRestore() {
  permissionFailure = 500
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() =>
    expect(document.body.textContent).not.toContain('Publication could not be confirmed'),
  )
  expect(document.body.textContent).not.toContain('Verified original capacity')
  permissionFailure = 0
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() =>
    expect(document.body.textContent).toContain('Publication could not be confirmed'),
  )
}
it('retains existing availability uncertainty without displaying it during fresh parent authority failure', async () => {
  await render()
  const toggle = host.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Enable model"]',
  )!
  await act(async () => toggle.click())
  await click('Save configuration')
  await until(() => expect(host.textContent).toContain('Publication could not be confirmed'))
  const written = requests.filter((request) => request.method === 'patch')
  expect(written).toHaveLength(1)
  await loseAndRestore()
  expect(
    host.querySelector('[role="switch"][aria-label="Enable model"]')!.getAttribute('aria-checked'),
  ).toBe('false')
  expect(requests.filter((request) => request.method === 'patch')).toHaveLength(1)
})
it('retains exact existing capacity intent across fresh parent authority hiding without automatic replay', async () => {
  await render()
  await until(() => expect(host.textContent).toContain('Edit capacity attestation'))
  await click('Edit capacity attestation')
  await until(() =>
    expect(
      document.querySelector('input[aria-label="Maximum billable input tokens"]'),
    ).not.toBeNull(),
  )
  await fill('Maximum billable input tokens', '128000')
  await fill('Maximum billable output tokens', '2048')
  await fill('Capacity evidence', 'Verified original capacity')
  await fill('Reason for attestation', 'Retain exact original review')
  await click('Save attestation')
  await until(() =>
    expect(document.body.textContent).toContain('Publication could not be confirmed'),
  )
  const first = requests.find((request) => request.method === 'put')!
  await loseAndRestore()
  expect(
    document.querySelector<HTMLInputElement>('input[aria-label="Capacity evidence"]')!.value,
  ).toBe('Verified original capacity')
  await click('Retry publication')
  await until(() => expect(requests.filter((request) => request.method === 'put')).toHaveLength(2))
  const retry = requests.filter((request) => request.method === 'put')[1]
  expect(retry.data).toBe(first.data)
  expect(retry.headers.get('If-Match')).toBe(first.headers.get('If-Match'))
})
