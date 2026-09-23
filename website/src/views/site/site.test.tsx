import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n, { applyDefaultLanguage, languageStorageKey, setLanguagePreference } from '@/i18n'
import en from '@/i18n/locales/en/site'
import zh from '@/i18n/locales/zh/site'
import client from '@/api/client'
import { siteKey } from '@/api/site'
import { SitePresentation, SiteLogo, SiteFooter } from '@/components/app/SiteBranding'
import type { SiteSettings } from '@/types/site'
import SitePage from './index'
import { normalizeSite, validSite } from './validation'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient, requests: InternalAxiosRequestConfig[]
let permissions: string[], status: number, site: SiteSettings, readFailure: boolean
const original = client.defaults.adapter
beforeEach(async () => {
  i18n.addResourceBundle('en', 'site', en, true, true)
  i18n.addResourceBundle('zh', 'site', zh, true, true)
  localStorage.clear()
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['system.read', 'site.write']
  status = 0
  readFailure = false
  site = {
    name: 'RouteX',
    service_url: '',
    logo_url: '',
    footer: '',
    default_language: 'en',
    etag: 'rev_initial',
    updated_at: '2026-09-23T00:00:00Z',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
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
      response.data = { user: { id: 'usr_admin', role: 'admin' }, csrf_token: 'csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/site') {
      if (readFailure)
        throw new AxiosError('fixture failure', '', config, undefined, { ...response, status: 503 })
      response.data = { ...site }
    } else if (config.url === '/admin/site') {
      if (status)
        throw new AxiosError('fixture failure', '', config, undefined, { ...response, status })
      const input = JSON.parse(config.data)
      expect(Object.keys(input).sort()).toEqual([
        'default_language',
        'etag',
        'footer',
        'logo_url',
        'name',
        'service_url',
      ])
      site = { ...site, ...input, etag: 'rev_saved' }
      response.data = { ...site }
    } else throw new Error(`Unexpected fixture URL ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  localStorage.clear()
  await i18n.changeLanguage('en')
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}
async function render(page = <SitePage />) {
  await act(async () =>
    root.render(<QueryClientProvider client={cache}>{page}</QueryClientProvider>),
  )
  await settle()
  await settle()
}
async function input(name: string, value: string) {
  const element = host.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
    element.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await settle()
}
function button(text: string) {
  return [...host.querySelectorAll('button')].find((b) => b.textContent === text)!
}

describe('site settings', () => {
  it('saves dirty normalized presentation fields with CSRF and a strict payload', async () => {
    await render()
    expect(button('Save changes').disabled).toBe(true)
    await input('name', '  Example Console  ')
    await input('service_url', 'https://console.example.invalid///')
    expect(host.textContent).toContain('Unsaved changes')
    await submit()
    expect(site.name).toBe('Example Console')
    expect(site.service_url).toBe('https://console.example.invalid')
    expect(requests.find((r) => r.url === '/admin/site')?.headers.get('X-CSRF-Token')).toBe('csrf')
    expect(host.textContent).toContain('System information saved.')
    expect(button('Save changes').disabled).toBe(true)
    expect(host.textContent).not.toContain('Unsaved changes')
  })
  it('preserves drafts on errors, then requires conflict review before resubmitting', async () => {
    await render()
    await input('name', 'My draft')
    status = 503
    await submit()
    expect(host.querySelector<HTMLInputElement>('[name="name"]')!.value).toBe('My draft')
    expect(host.textContent).toContain('Your changes have been kept.')
    status = 409
    await submit()
    expect(button('Save changes').disabled).toBe(true)
    site = { ...site, name: 'Other administrator', etag: 'rev_other' }
    readFailure = true
    await act(async () => button('Review latest version').click())
    await settle()
    expect(host.querySelector<HTMLInputElement>('[name="name"]')!.value).toBe('My draft')
    readFailure = false
    await act(async () => button('Review latest version').click())
    await settle()
    expect(host.textContent).toContain('Other administrator')
    expect(button('Save changes').disabled).toBe(false)
    status = 0
    await submit()
    expect(JSON.parse(requests.filter((r) => r.url === '/admin/site').at(-1)!.data).etag).toBe(
      'rev_other',
    )
  })
  it('gates read access before querying and keeps read-only fields disabled', async () => {
    permissions = []
    await render()
    expect(requests.some((r) => r.url === '/site')).toBe(false)
    permissions = ['system.read']
    await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
    await settle()
    await settle()
    expect(host.querySelector('fieldset')?.disabled).toBe(true)
    expect(button('Save changes')).toBeUndefined()
    expect(requests.some((r) => r.url === '/admin/site')).toBe(false)
  })
  it('localizes fields and validates Unicode limits and unsafe URL schemes', async () => {
    await i18n.changeLanguage('zh')
    await render()
    expect(host.textContent).toContain('系统信息')
    expect(host.textContent).toContain('默认语言')
    expect(validSite({ ...site, name: '😀'.repeat(100), footer: '😀'.repeat(500) })).toBe(true)
    for (const name of ['', 'a'.repeat(101), 'name\nnext'])
      expect(validSite({ ...site, name })).toBe(false)
    for (const url of [
      'javascript:alert(1)',
      'data:image/svg+xml,test',
      'https://user:pass@example.invalid/',
      'https://example.invalid/#fragment',
    ])
      expect(validSite({ ...site, logo_url: url })).toBe(false)
    expect(Object.keys(normalizeSite(site))).not.toContain('updated_at')
  })
})
describe('site presentation', () => {
  it('applies title, literal footer and safe image with fallback while leaving language preference unset', async () => {
    site = {
      ...site,
      name: 'Example Console',
      footer: '<script>literal</script>',
      logo_url: 'https://images.example.invalid/logo.png',
      default_language: 'zh',
    }
    await render(
      <>
        <SitePresentation />
        <SiteLogo />
        <SiteFooter />
      </>,
    )
    expect(document.title).toBe('Example Console')
    expect(i18n.resolvedLanguage).toBe('zh')
    expect(localStorage.getItem(languageStorageKey)).toBeNull()
    expect(host.querySelector('script')).toBeNull()
    const logo = host.querySelector('img')!
    expect(logo.getAttribute('referrerpolicy')).toBe('no-referrer')
    await act(async () => logo.dispatchEvent(new Event('error')))
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('svg')).not.toBeNull()
    readFailure = true
    await act(async () => cache.invalidateQueries({ queryKey: siteKey }))
    await settle()
    expect(i18n.resolvedLanguage).toBe('zh')
    expect(document.title).toBe('Example Console')
  })
  it('never overrides or rewrites an explicit language preference', async () => {
    await setLanguagePreference('en')
    site.default_language = 'zh'
    await render(<SitePresentation />)
    expect(i18n.resolvedLanguage).toBe('en')
    expect(localStorage.getItem(languageStorageKey)).toBe('en')
    const pending = applyDefaultLanguage('en')
    await setLanguagePreference('zh')
    await pending
    expect(i18n.resolvedLanguage).toBe('zh')
    expect(localStorage.getItem(languageStorageKey)).toBe('zh')
  })
  it('rejects active image schemes even if unexpected API data includes them', async () => {
    site.logo_url = 'javascript:alert(1)'
    await render(<SiteLogo />)
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('svg')).not.toBeNull()
  })
})
