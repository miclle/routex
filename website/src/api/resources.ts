import client from './client'
import type {
  ResourceCandidate,
  ResourceFilters,
  ResourceKind,
  ResourceList,
  ResourceRecord,
} from '@/types/resources'
export const resourcePath = (kind: ResourceKind, admin: boolean) =>
  `${admin ? '/admin' : ''}/${kind}`
export async function getResources(
  kind: ResourceKind,
  admin: boolean,
  filters: ResourceFilters,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<ResourceList>(resourcePath(kind, admin), {
      params: { ...filters, cursor: cursor || undefined },
      signal,
    })
  ).data
}
export async function getResource(
  kind: ResourceKind,
  admin: boolean,
  id: string,
  signal?: AbortSignal,
) {
  return (await client.get<ResourceRecord>(`${resourcePath(kind, admin)}/${id}`, { signal })).data
}
export async function getResourceCandidates(path: string, q: string, signal?: AbortSignal) {
  return (await client.get<{ items: ResourceCandidate[] }>(path, { params: { q }, signal })).data
    .items
}
