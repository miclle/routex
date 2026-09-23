export const maxCSVBytes = 32 * 1024
export class PriceFileError extends Error {}
export async function readPriceFile(file: File) {
  if (!/\.csv$/i.test(file.name)) throw new PriceFileError('fileType')
  if (file.size > maxCSVBytes) throw new PriceFileError('fileSize')
  const bytes = await new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(new PriceFileError('fileRead'))
    reader.readAsArrayBuffer(file)
  })
  if (bytes.byteLength > maxCSVBytes) throw new PriceFileError('fileSize')
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    throw new PriceFileError('fileEncoding')
  }
}
