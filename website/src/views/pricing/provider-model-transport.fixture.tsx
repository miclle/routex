import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { expect } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import AuthGate from '@/components/app/AuthGate'
import type { ProviderModel } from '@/types/catalog'
import type { ProviderModelCapacity } from '@/types/provider-model-capacity'
import ProviderModelPage from './provider-model'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
export async function transportFixture() {
  await i18n.changeLanguage('en')
  const host = document.createElement('div')
  document.body.append(host)
  const root: Root = createRoot(host),
    cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const state = {
    actor: 'usr_transport',
    csrf: 'csrf-original',
    permissions: ['providers.read', 'providers.write'],
    sessionStatus: 200,
    permissionStatus: 200,
    catalogueStatus: 200,
    failure: 0,
    hold: undefined as Promise<void> | undefined,
    ignoreAbort: false,
    revision: 0,
    model: {
      id: 'pmo_one',
      upstream_name: 'Retained Model',
      etag: '0',
      enabled: true,
      supports_image_input: true,
      supports_pdf_input: false,
      capabilities_transport_current: false,
      capability_review_etag: 'a'.repeat(64),
    } as ProviderModel,
    capacity: {
      provider_model_id: 'pmo_one',
      protocol: 'openai_chat',
      etag: 'b'.repeat(64),
      revision: '0',
      transport_current: false,
      configured: false,
      max_input_tokens: 0,
      max_output_tokens: 0,
      evidence: '',
      updated_at: '0001-01-01T00:00:00Z',
    } as ProviderModelCapacity,
    requests: [] as InternalAxiosRequestConfig[],
  }
  const providers = () => [
    {
      id: 'prv_one',
      name: 'Provider',
      connections: [
        {
          id: 'con_one',
          name: 'Connection',
          protocol: 'openai_chat',
          adapter: 'native',
          api_version: null,
          base_url: 'https://example.invalid/v1',
          enabled: false,
          credentials: [],
          provider_models: [structuredClone(state.model)],
        },
      ],
    },
  ]
  client.defaults.adapter = async (config) => {
    state.requests.push(config)
    const response = {
      config,
      data: {} as unknown,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    const reject = (status: number) => {
      response.status = status
      throw new AxiosError('Controlled transport response', '', config, undefined, response)
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      if (state.sessionStatus !== 200) reject(state.sessionStatus)
      response.data = {
        user: { id: state.actor, role: 'member', name: 'Actor', email: 'actor@example.invalid' },
        csrf_token: state.csrf,
      }
    } else if (config.url === '/auth/permissions') {
      if (state.permissionStatus !== 200) reject(state.permissionStatus)
      response.data = { permissions: state.permissions }
    } else if (config.url === '/admin/providers') {
      if (state.catalogueStatus !== 200) reject(state.catalogueStatus)
      response.data = { items: providers() }
    } else if (config.url === '/admin/provider-models/pmo_one' && config.method === 'patch') {
      const input = JSON.parse(config.data)
      if (state.hold) await state.hold
      if (state.ignoreAbort) config.signal = undefined
      if (!state.failure || state.failure === 503) {
        state.revision++
        state.model = {
          ...state.model,
          etag: `pms_${String(state.revision).padStart(26, '0')}`,
          capability_review_etag: String(state.revision + 2)
            .repeat(64)
            .slice(0, 64),
          ...(input.enabled === undefined ? {} : { enabled: input.enabled }),
          ...(input.capability_review_etag === undefined
            ? {}
            : {
                supports_image_input: input.supports_image_input,
                supports_pdf_input: input.supports_pdf_input,
                capabilities_transport_current: true,
              }),
        }
      }
      if (state.failure) {
        if (state.failure === -1) throw new AxiosError('Network outcome unknown', '', config)
        reject(state.failure)
      }
      response.data = structuredClone(state.model)
    } else if (config.url === '/admin/provider-models/pmo_one/reservation-bound') {
      if (config.method === 'put') {
        const input = JSON.parse(config.data)
        if (state.hold) await state.hold
        if (state.ignoreAbort) config.signal = undefined
        if (!state.failure || state.failure === 503) {
          state.revision++
          state.capacity = {
            ...state.capacity,
            max_input_tokens: input.max_input_tokens,
            max_output_tokens: input.max_output_tokens,
            evidence: input.evidence,
            configured: true,
            transport_current: true,
            revision: `bnd_${String(state.revision).padStart(26, '0')}`,
            etag: String(state.revision + 3)
              .repeat(64)
              .slice(0, 64),
            updated_at: '2026-10-09T02:00:00Z',
          }
        }
        if (state.failure) {
          if (state.failure === -1) throw new AxiosError('Network outcome unknown', '', config)
          reject(state.failure)
        }
      }
      response.data = structuredClone(state.capacity)
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${state.capacity.etag}"`)
    } else throw new Error(`Unexpected fixture request ${config.url}`)
    return response
  }
  const router = createMemoryRouter(
    [
      {
        element: (
          <UncertainIntentProvider>
            <AuthGate mode="private" />
          </UncertainIntentProvider>
        ),
        children: [
          { path: '/admin/providers/:providerId/models/:modelId', element: <ProviderModelPage /> },
          { path: '/other', element: <p>Other route</p> },
        ],
      },
      { path: '/login', element: <p>Login</p> },
    ],
    { initialEntries: ['/admin/providers/prv_one/models/pmo_one'] },
  )
  const mount = async () => {
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() =>
      expect(host.textContent).toContain(
        state.permissions.includes('providers.read')
          ? 'Provider-side availability'
          : 'Provider model details',
      ),
    )
  }
  const dispose = async () => {
    await act(async () => root.unmount())
    router.dispose()
    cache.clear()
    host.remove()
    client.defaults.adapter = original
    await i18n.changeLanguage('en')
  }
  const writes = () => state.requests.filter((r) => r.method === 'patch' || r.method === 'put')
  const refreshCatalogue = async () => {
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
    })
  }
  const renew = async () => {
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
  }
  return { state, host, root, cache, router, mount, dispose, writes, refreshCatalogue, renew }
}
export async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
export function button(label: string) {
  const result = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(result, `Missing button ${label}`).toBeTruthy()
  return result!
}
export async function click(label: string) {
  await act(async () => button(label).click())
}
export async function fill(label: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  expect(input).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
export async function toggle(label: string) {
  const control = document.querySelector<HTMLButtonElement>(
    `[role="switch"][aria-label="${label}"]`,
  )!
  expect(control).toBeTruthy()
  await act(async () => control.click())
}
