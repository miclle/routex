import axios from 'axios'
import { t } from '@/i18n'
import type { ModelCatalogRecord } from '@/types/model-catalog'
import client from './client'
import { catalogError } from './catalog'

export async function listModelCatalog(signal?: AbortSignal) {
  return (await client.get<{ items: ModelCatalogRecord[] }>('/model-catalog', { signal })).data
    .items
}

export async function getModelCatalogRecord(id: string, signal?: AbortSignal) {
  const record = (
    await client.get<ModelCatalogRecord>(`/model-catalog/${encodeURIComponent(id)}`, { signal })
  ).data
  if (record.id !== id) throw new Error('Unexpected model catalogue resource')
  return record
}

export function modelCatalogError(error: unknown) {
  return axios.isAxiosError(error) && error.response?.status === 422
    ? t('catalog:memberModels.catalogueOverflow')
    : catalogError(error)
}
