import client from './client'
import type { AuditFilters, AuditPage } from '@/types/audit'
export async function listAudit(
  filters: AuditFilters,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<AuditPage>('/admin/audit', {
      params: { ...filters, q: filters.q.trim() || undefined, cursor: cursor ?? undefined },
      signal,
    })
  ).data
}
