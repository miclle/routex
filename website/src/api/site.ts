import client from './client'
import { writeCatalog } from './catalog'
import type { SiteSettings, SiteInput, Announcement, AnnouncementPage } from '@/types/site'
export const siteKey = ['site'] as const
export async function getSite(signal?: AbortSignal) {
  const data = (await client.get<SiteSettings>('/site', { signal })).data
  if (
    !data ||
    !['name', 'service_url', 'logo_url', 'footer', 'etag', 'updated_at'].every(
      (key) => typeof data[key as keyof SiteSettings] === 'string',
    ) ||
    !['en', 'zh'].includes(data.default_language)
  )
    throw new Error('Invalid site settings response')
  return data
}
export function saveSite(input: SiteInput, csrf: string) {
  return writeCatalog<SiteSettings>('put', '/admin/site', input, csrf)
}
export async function getAnnouncements(admin: boolean, cursor?: string, signal?: AbortSignal) {
  const data = (
    await client.get<AnnouncementPage>(admin ? '/admin/announcements' : '/announcements', {
      params: cursor ? { cursor } : undefined,
      signal,
    })
  ).data
  if (
    !data ||
    !Array.isArray(data.items) ||
    data.items.some(
      (item) =>
        !item ||
        typeof item.id !== 'string' ||
        typeof item.content !== 'string' ||
        !['active', 'closed'].includes(item.status),
    )
  )
    throw new Error('Invalid announcement response')
  return data
}
export function publishAnnouncement(content: string, csrf: string) {
  return writeCatalog<Announcement>('post', '/admin/announcements', { content }, csrf)
}
export function editAnnouncement(id: string, content: string, etag: string, csrf: string) {
  return writeCatalog<Announcement>(
    'patch',
    `/admin/announcements/${encodeURIComponent(id)}`,
    { content, etag },
    csrf,
  )
}
export function closeAnnouncement(id: string, etag: string, csrf: string) {
  return writeCatalog<Announcement>(
    'post',
    `/admin/announcements/${encodeURIComponent(id)}/close`,
    { etag },
    csrf,
  )
}
