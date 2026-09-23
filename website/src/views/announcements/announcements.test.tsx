import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/announcements'
import zh from '@/i18n/locales/zh/announcements'
import client from '@/api/client'
import { AnnouncementFeed } from '@/components/app/AnnouncementFeed'
import type { Announcement } from '@/types/site'
import AnnouncementsPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient, requests: InternalAxiosRequestConfig[]
let permissions: string[],
  failure: number,
  records: Announcement[],
  signedIn: boolean,
  pagination: boolean
const original = client.defaults.adapter
beforeEach(async () => {
  i18n.addResourceBundle('en', 'announcements', en, true, true)
  i18n.addResourceBundle('zh', 'announcements', zh, true, true)
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['system.read', 'announcements.write']
  failure = 0
  signedIn = true
  pagination = false
  records = [
    {
      id: 'ann_active',
      content: 'Current maintenance notice',
      status: 'active',
      etag: 'rev_one',
      created_at: '2026-09-23T00:00:00Z',
      updated_at: '2026-09-23T00:00:00Z',
      closed_at: null,
    },
    {
      id: 'ann_closed',
      content: 'Previous notice',
      status: 'closed',
      etag: 'rev_closed',
      created_at: '2026-09-22T00:00:00Z',
      updated_at: '2026-09-22T00:00:00Z',
      closed_at: '2026-09-22T00:00:00Z',
    },
  ]
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
      response.data = signedIn
        ? { user: { id: 'usr_self', role: 'member' }, csrf_token: 'csrf' }
        : null
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.method === 'get')
      response.data =
        config.url === '/announcements'
          ? { items: records.filter((r) => r.status === 'active'), next_cursor: null }
          : {
              items: config.params?.cursor ? [records[1]] : records,
              next_cursor: pagination && !config.params?.cursor ? 'ann_cursor' : null,
            }
    else {
      if (failure)
        throw new AxiosError('fixture failure', '', config, undefined, {
          ...response,
          status: failure,
        })
      const input = JSON.parse(config.data)
      expect(config.headers.get('X-CSRF-Token')).toBe('csrf')
      if (config.url === '/admin/announcements') {
        const record = { ...records[0], id: 'ann_new', content: input.content }
        records = [record, ...records]
        response.data = record
      } else {
        const id = config.url!.split('/')[3]
        const record = records.find((r) => r.id === id)!
        expect(input.etag).toBe(record.etag)
        if (config.url?.endsWith('/close')) {
          record.status = 'closed'
          record.closed_at = '2026-09-23T00:00:01Z'
        } else record.content = input.content
        record.etag = 'rev_updated'
        response.data = { ...record }
      }
    }
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
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}
async function render(page = <AnnouncementsPage />) {
  await act(async () =>
    root.render(<QueryClientProvider client={cache}>{page}</QueryClientProvider>),
  )
  await settle()
  await settle()
}
function button(text: string, scope: ParentNode = document) {
  return [...scope.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === text,
  )!
}
async function click(text: string, scope: ParentNode = document) {
  await act(async () => button(text, scope).click())
  await settle()
}
async function input(value: string) {
  const field = document.querySelector('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await settle()
}
function row(content: string) {
  return [...host.querySelectorAll('tbody tr')].find((r) => r.textContent?.includes(content))!
}

describe('announcements', () => {
  it('publishes plain text, preserves history and clears the dialog after success', async () => {
    await render()
    expect(host.textContent).toContain('Previous notice')
    await click('Publish announcement')
    await input('  <script>literal</script>\nMaintenance tonight  ')
    await submit()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).toContain('Announcement published.')
    expect(host.textContent).toContain('<script>literal</script>')
    expect(host.querySelector('script')).toBeNull()
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data).content).toBe(
      '<script>literal</script>\nMaintenance tonight',
    )
  })
  it('edits closed history without reopening and confirms closure of active records', async () => {
    await render()
    expect(button('Close', row('Previous notice'))).toBeUndefined()
    await click('Edit', row('Previous notice'))
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'Original announcement',
    )
    await input('Revised history')
    await submit()
    expect(records[1].status).toBe('closed')
    await click('Close', row('Current maintenance notice'))
    expect(requests.some((r) => r.url?.endsWith('/close'))).toBe(false)
    await click('Confirm close')
    expect(records[0].status).toBe('closed')
    expect(host.textContent).toContain('Announcement closed.')
  })
  it('keeps edits on failure and requires current-version review after conflict', async () => {
    await render()
    await click('Edit', row('Current maintenance notice'))
    await input('My draft')
    failure = 503
    await submit()
    expect(document.querySelector('textarea')?.value).toBe('My draft')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('kept')
    failure = 409
    await submit()
    expect(button('Save').disabled).toBe(true)
    records[0] = { ...records[0], content: 'Someone else changed this', etag: 'rev_concurrent' }
    await click('Review latest version')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'Someone else changed this',
    )
    expect(document.querySelector('textarea')?.value).toBe('My draft')
    failure = 0
    await submit()
    expect(records[0].content).toBe('My draft')
  })
  it('rejects empty and oversized content but accepts 4,000 Unicode characters', async () => {
    await render()
    await click('Publish announcement')
    await input('   ')
    await submit()
    await input('😀'.repeat(4001))
    await submit()
    expect(requests.filter((r) => r.method === 'post')).toHaveLength(0)
    await input('😀'.repeat(4000))
    await submit()
    expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
  })
  it('gates history reads and writes by current permission and paginates actual cursors', async () => {
    permissions = []
    await render()
    expect(requests.some((r) => r.url === '/admin/announcements')).toBe(false)
    permissions = ['system.read']
    pagination = true
    await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
    await settle()
    await settle()
    expect(button('Publish announcement')).toBeUndefined()
    expect(button('Edit')).toBeUndefined()
    await click('Next')
    expect(requests.at(-1)?.params).toEqual({ cursor: 'ann_cursor' })
    expect(host.textContent).toContain('Page 2')
    await click('Previous')
    expect(host.textContent).toContain('Current maintenance notice')
  })
  it('localizes announcement dialogs and does not send a canceled draft', async () => {
    await i18n.changeLanguage('zh')
    await render()
    expect(host.textContent).toContain('公告记录')
    await click('发布公告')
    await input('Draft')
    await click('取消')
    await click('发布公告')
    expect(document.querySelector('textarea')?.value).toBe('')
    expect(requests.some((r) => r.method === 'post')).toBe(false)
  })
  it('reads only active notices for signed-in members and never exposes closed history', async () => {
    await render(<AnnouncementFeed />)
    expect(host.textContent).toContain('Current maintenance notice')
    expect(host.textContent).not.toContain('Previous notice')
    expect(requests.some((r) => r.url === '/admin/announcements')).toBe(false)
    await act(async () => cache.setQueryData(['auth', 'session'], null))
    await settle()
    expect(host.textContent).toBe('')
  })
  it('does not query member announcements before authentication', async () => {
    signedIn = false
    await render(<AnnouncementFeed />)
    expect(requests.some((r) => r.url === '/announcements')).toBe(false)
  })
})
