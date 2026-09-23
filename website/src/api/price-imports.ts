import client from './client'
import type {
  PriceImportPreview,
  PriceImportCommit,
  PriceImportDocument,
} from '@/types/price-imports'
export async function previewPriceDocument(document: PriceImportDocument, csrf: string) {
  return (
    await client.post<PriceImportPreview>('/admin/prices/import/preview', document, {
      headers: { 'X-CSRF-Token': csrf },
    })
  ).data
}
export async function commitPriceDocument(
  document: PriceImportDocument,
  preview: PriceImportPreview,
  csrf: string,
) {
  return (
    await client.post<PriceImportCommit>(
      '/admin/prices/import/commit',
      { ...document, etag: preview.etag, preview_digest: preview.preview_digest },
      { headers: { 'X-CSRF-Token': csrf } },
    )
  ).data
}
export async function exportPriceCSV() {
  return (await client.get<Blob>('/admin/prices/export.csv', { responseType: 'blob' })).data
}
export function downloadPriceCSV(blob: Blob) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = 'routex-prices.csv'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
