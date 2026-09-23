import client from './client'
import type { UsageFilters, UsageReport, UsageScope } from '@/types/usage'

export async function getUsage(
  scope: UsageScope,
  filters: UsageFilters,
  signal?: AbortSignal,
): Promise<UsageReport> {
  return (
    await client.get<UsageReport>(
      scope.projectId
        ? `/projects/${encodeURIComponent(scope.projectId)}/usage`
        : scope.admin
          ? '/admin/usage'
          : '/usage',
      { params: filters, signal },
    )
  ).data
}
