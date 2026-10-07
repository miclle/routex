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
const workbookMIME = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
export async function exportPriceCSV(signal?: AbortSignal) {
  return (await client.get<Blob>('/admin/prices/export.csv', { responseType: 'blob', signal })).data
}
export async function exportPriceXLSX(signal?: AbortSignal) {
  const blob = (
    await client.get<Blob>('/admin/prices/export.xlsx', { responseType: 'blob', signal })
  ).data
  if (
    !(blob instanceof Blob) ||
    blob.type.split(';')[0] !== workbookMIME ||
    blob.size === 0 ||
    blob.size > 512 * 1024
  ) {
    throw new Error('Invalid price workbook response')
  }
  return blob
}
function downloadPriceFile(blob: Blob, filename: 'routex-prices.csv' | 'routex-prices.xlsx') {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.append(anchor)
  try {
    anchor.click()
  } finally {
    anchor.remove()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
export function downloadPriceCSV(blob: Blob) {
  downloadPriceFile(blob, 'routex-prices.csv')
}
export function downloadPriceXLSX(blob: Blob) {
  downloadPriceFile(blob, 'routex-prices.xlsx')
}
