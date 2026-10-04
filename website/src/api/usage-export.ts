import client from './client'
import type { UsageFilters, UsageScope } from '@/types/usage'

const maximumBytes = 8 * 1024 * 1024
const identity = (value: string) => /^[A-Za-z0-9_-]{1,30}$/.test(value)

export function usageExportFilename(scope: UsageScope) {
  return `routex-${scope.teamId ? 'team' : scope.projectId ? 'project' : scope.admin ? 'platform' : 'personal'}-usage.csv`
}

// Export a new server-owned report; never serialize the browser's cached report.
export async function exportUsage(
  scope: UsageScope,
  filters: UsageFilters,
  signal: AbortSignal,
): Promise<Blob> {
  if (
    (scope.teamId && (scope.projectId || scope.admin || !identity(scope.teamId))) ||
    (scope.projectId && (scope.admin || !identity(scope.projectId)))
  )
    throw new Error('Invalid usage export scope')
  const ordinary = [
    'period',
    'from',
    'to',
    'timezone',
    'granularity',
    'compare',
    'model_id',
    'status',
    'protocol',
    'stream',
  ]
  const allowed = [
    ...ordinary,
    ...(!scope.teamId ? ['key_id'] : []),
    ...(scope.admin
      ? ['user_id', 'project_id', 'team_id', 'provider_id', 'provider_model_id', 'connection_id']
      : []),
  ]
  if (Object.keys(filters).some((key) => !allowed.includes(key)))
    throw new Error('Invalid usage export filters')
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  try {
    const response = await client.get<Blob>(
      `${scope.teamId ? `/teams/${encodeURIComponent(scope.teamId)}` : scope.projectId ? `/projects/${encodeURIComponent(scope.projectId)}` : scope.admin ? '/admin' : ''}/usage/export.csv`,
      {
        params: { ...filters },
        responseType: 'blob',
        signal: controller.signal,
        onDownloadProgress: ({ loaded, total }) => {
          if (loaded > maximumBytes || (total !== undefined && total > maximumBytes)) abort()
        },
      },
    )
    if (signal.aborted || controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
    const contentType = String(response.headers['content-type'] ?? '')
    const length = response.headers['content-length']
    if (
      response.status !== 200 ||
      !/^text\/csv\s*;\s*charset\s*=\s*utf-8\s*$/i.test(contentType) ||
      !(response.data instanceof Blob) ||
      response.data.size === 0 ||
      response.data.size > maximumBytes ||
      (length !== undefined && (!/^\d+$/.test(String(length)) || Number(length) > maximumBytes))
    )
      throw new Error('Invalid usage export response')
    return response.data
  } finally {
    signal.removeEventListener('abort', abort)
  }
}

export function downloadUsageCSV(blob: Blob, scope: UsageScope) {
  const url = URL.createObjectURL(blob)
  try {
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = usageExportFilename(scope)
    document.body.append(anchor)
    try {
      anchor.click()
    } finally {
      anchor.remove()
    }
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
