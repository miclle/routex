import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { exportPriceCSV, exportPriceXLSX, downloadPriceXLSX } from './price-imports'

const mime = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
afterEach(() => vi.restoreAllMocks())
describe('price export binary transport', () => {
  it('requests only the exact XLSX endpoint and retains its original Blob with cancellation', async () => {
    const blob = new Blob([new Uint8Array([80, 75, 3, 4])], { type: mime })
    const get = vi.spyOn(client, 'get').mockResolvedValue({ data: blob })
    const signal = new AbortController().signal
    expect(await exportPriceXLSX(signal)).toBe(blob)
    expect(get).toHaveBeenCalledExactlyOnceWith('/admin/prices/export.xlsx', {
      responseType: 'blob',
      signal,
    })
  })
  it('forwards cancellation on the unchanged CSV endpoint', async () => {
    const blob = new Blob(['amount\n0\n'], { type: 'text/csv' })
    const get = vi.spyOn(client, 'get').mockResolvedValue({ data: blob })
    const signal = new AbortController().signal
    expect(await exportPriceCSV(signal)).toBe(blob)
    expect(get).toHaveBeenCalledExactlyOnceWith('/admin/prices/export.csv', {
      responseType: 'blob',
      signal,
    })
  })
  it.each([
    new Blob([], { type: mime }),
    new Blob(['not a workbook'], { type: 'text/html' }),
    new Blob([new Uint8Array(512 * 1024 + 1)], { type: mime }),
  ])('rejects an empty, wrong MIME or oversized success without downloading', async (blob) => {
    vi.spyOn(client, 'get').mockResolvedValue({ data: blob })
    await expect(exportPriceXLSX()).rejects.toThrow()
  })
  it('retains the exact admitted 512 KiB Blob without numeric or workbook rewriting', async () => {
    const blob = new Blob([new Uint8Array(512 * 1024)], { type: mime })
    vi.spyOn(client, 'get').mockResolvedValue({ data: blob })
    expect(await exportPriceXLSX()).toBe(blob)
  })
  it('propagates rejected requests without serializing a binary error', async () => {
    const error = new Error('unavailable')
    vi.spyOn(client, 'get').mockRejectedValue(error)
    await expect(exportPriceXLSX()).rejects.toBe(error)
  })
  it('uses a literal XLSX filename, then removes the anchor and releases its URL', () => {
    const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:workbook')
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.useFakeTimers()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      expect(this.download).toBe('routex-prices.xlsx')
      expect(this.href).toBe('blob:workbook')
    })
    const blob = new Blob(['opaque bytes'], { type: mime })
    try {
      downloadPriceXLSX(blob)
      expect(create).toHaveBeenCalledExactlyOnceWith(blob)
      expect(click).toHaveBeenCalledTimes(1)
      expect(document.querySelector('a[download="routex-prices.xlsx"]')).toBeNull()
      vi.runAllTimers()
      expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:workbook')
    } finally {
      vi.useRealTimers()
    }
  })
})
