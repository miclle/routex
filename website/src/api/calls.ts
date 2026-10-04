import client from './client'
import type { AdminCallDetail, CallFilters, CallPage, CallRecord } from '@/types/calls'

export async function listCalls(
  admin: boolean,
  filters: CallFilters,
  cursor: string | null,
  signal?: AbortSignal,
  projectId?: string,
  teamId?: string,
): Promise<CallPage> {
  return (
    await client.get<CallPage>(
      teamId
        ? `/teams/${encodeURIComponent(teamId)}/calls`
        : projectId
          ? `/projects/${encodeURIComponent(projectId)}/calls`
          : admin
            ? '/admin/calls'
            : '/calls',
      {
        params: { ...filters, cursor: cursor ?? undefined, limit: 40 },
        signal,
      },
    )
  ).data
}
export async function getCall(
  admin: boolean,
  requestId: string,
  signal?: AbortSignal,
  projectId?: string,
  teamId?: string,
): Promise<CallRecord | AdminCallDetail> {
  return (
    await client.get<CallRecord | AdminCallDetail>(
      `${teamId ? `/teams/${encodeURIComponent(teamId)}/calls` : projectId ? `/projects/${encodeURIComponent(projectId)}/calls` : admin ? '/admin/calls' : '/calls'}/${encodeURIComponent(requestId)}`,
      { signal },
    )
  ).data
}

export async function exportCalls(
  admin: boolean,
  filters: CallFilters,
  signal?: AbortSignal,
  projectId?: string,
  teamId?: string,
): Promise<Blob> {
  if (teamId && (admin || projectId || !/^[A-Za-z0-9_-]{1,64}$/.test(teamId)))
    throw new Error('Invalid call export scope')
  if (
    teamId &&
    Object.keys(filters).some((name) => !['status', 'model_id', 'from', 'to'].includes(name))
  )
    throw new Error('Invalid Team call export filters')
  const response = await client.get<Blob>(
    teamId
      ? `/teams/${encodeURIComponent(teamId)}/calls/export.csv`
      : projectId
        ? `/projects/${encodeURIComponent(projectId)}/calls/export.csv`
        : admin
          ? '/admin/calls/export.csv'
          : '/calls/export.csv',
    { params: { ...filters }, responseType: 'blob', signal },
  )
  if (signal?.aborted) throw new Error('Call export canceled')
  if (
    teamId &&
    (response.status !== 200 ||
      !(response.data instanceof Blob) ||
      response.data.size === 0 ||
      response.data.size > 8 * 1024 * 1024 ||
      !/^text\/csv(?:;\s*charset=utf-8)?$/i.test(String(response.headers['content-type'] ?? '')))
  )
    throw new Error('Invalid Team call export response')
  return response.data
}

export function downloadCallsCSV(blob: Blob, admin: boolean, projectId?: string, teamId?: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = teamId
    ? 'routex-team-calls.csv'
    : projectId
      ? 'routex-project-calls.csv'
      : admin
        ? 'routex-platform-calls.csv'
        : 'routex-personal-calls.csv'
  try {
    document.body.append(anchor)
    anchor.click()
  } finally {
    anchor.remove()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
