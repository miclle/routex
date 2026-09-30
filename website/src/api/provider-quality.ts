import axios from 'axios'
import client from './client'
import type {
  ProviderQualityPolicy,
  ProviderQualitySummary,
  UpdateProviderQualityPolicyInput,
} from '@/types/provider-quality'

export const providerQualityKey = (providerId: string) =>
  ['admin', 'providers', providerId, 'quality'] as const
export const providerQualityPolicyKey = (providerId: string) =>
  ['admin', 'providers', providerId, 'quality-policy'] as const

export class ProviderQualityError extends Error {
  constructor(public readonly status: number) {
    super('Provider quality request failed')
  }
}

function statusOf(error: unknown) {
  return axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0
}

export async function getProviderQuality(providerId: string, signal?: AbortSignal) {
  return (
    await client.get<ProviderQualitySummary>(
      `/admin/providers/${encodeURIComponent(providerId)}/quality`,
      { signal },
    )
  ).data
}

export async function getProviderQualityPolicy(providerId: string, signal?: AbortSignal) {
  const response = await client.get<ProviderQualityPolicy>(
    `/admin/providers/${encodeURIComponent(providerId)}/quality-policy`,
    { signal },
  )
  return {
    ...response.data,
    etag: response.data.etag || response.headers.etag || '',
  }
}

export async function updateProviderQualityPolicy(
  providerId: string,
  input: UpdateProviderQualityPolicyInput,
  csrf: string,
) {
  try {
    const response = await client.put<ProviderQualityPolicy>(
      `/admin/providers/${encodeURIComponent(providerId)}/quality-policy`,
      input,
      { headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag } },
    )
    return {
      ...response.data,
      etag: response.data.etag || response.headers.etag || '',
    }
  } catch (error) {
    throw new ProviderQualityError(statusOf(error))
  }
}
