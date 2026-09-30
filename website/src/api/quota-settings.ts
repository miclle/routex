import client from './client'
import type { QuotaSettings, QuotaSettingsInput } from '@/types/quota-settings'
export const quotaSettingsKey = ['admin', 'quota-settings'] as const
export async function getQuotaSettings(signal?: AbortSignal) {
  return (await client.get<QuotaSettings>('/admin/quota-settings', { signal })).data
}
export async function saveQuotaSettings(etag: string, input: QuotaSettingsInput, csrf: string) {
  return (
    await client.put<QuotaSettings>('/admin/quota-settings', input, {
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    })
  ).data
}
