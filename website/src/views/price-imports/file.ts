import type { PriceImportDocument } from '@/types/price-imports'
export const maxCSVBytes = 32 * 1024
export const maxWorkbookBytes = 512 * 1024
export class PriceFileError extends Error {}
export async function readPriceFile(file: File): Promise<PriceImportDocument> {
  const csv = /\.csv$/i.test(file.name)
  if (!csv && !/\.(xlsx|xls)$/i.test(file.name)) throw new PriceFileError('fileType')
  if (!csv && new TextEncoder().encode(file.name).length > 255) throw new PriceFileError('fileName')
  const maxBytes = csv ? maxCSVBytes : maxWorkbookBytes
  const sizeError = csv ? 'fileSize' : 'workbookSize'
  if (file.size > maxBytes) throw new PriceFileError(sizeError)
  const bytes = await new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(new PriceFileError('fileRead'))
    reader.readAsArrayBuffer(file)
  })
  if (bytes.byteLength > maxBytes) throw new PriceFileError(sizeError)
  if (!csv) {
    const view = new Uint8Array(bytes)
    let binary = ''
    for (let offset = 0; offset < view.length; offset += 32768) {
      binary += String.fromCharCode(...view.subarray(offset, offset + 32768))
    }
    return { filename: file.name, content_base64: btoa(binary) }
  }
  try {
    return { csv: new TextDecoder('utf-8', { fatal: true }).decode(bytes) }
  } catch {
    throw new PriceFileError('fileEncoding')
  }
}
