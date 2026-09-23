export interface SiteSettings {
  name: string
  service_url: string
  logo_url: string
  footer: string
  default_language: 'en' | 'zh'
  etag: string
  updated_at: string
}
export type SiteInput = Omit<SiteSettings, 'updated_at'>
export interface Announcement {
  id: string
  content: string
  status: 'active' | 'closed'
  etag: string
  created_at: string
  updated_at: string
  closed_at: string | null
}
export interface AnnouncementPage {
  items: Announcement[]
  next_cursor: string | null
}
