import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import ModelRenameFields from './model-rename-fields'
import { initialModelRenameDraft, modelCompatibilityDeadline } from './model-rename-draft'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.useRealTimers()
})
async function mount(oldName = 'Original') {
  function Form() {
    const [draft, setDraft] = useState(() => initialModelRenameDraft(oldName))
    return (
      <form>
        <ModelRenameFields oldName={oldName} draft={draft} onChange={setDraft} />
      </form>
    )
  }
  await act(async () => root.render(<Form />))
}
function expiration() {
  return host.querySelector<HTMLInputElement>('input[name="alias_expires_at"]')?.value
}

it('captures calendar-day presets at local end of day without mutating the review clock', () => {
  const now = new Date(2030, 0, 28, 12, 10, 5)
  const before = now.valueOf()
  expect(modelCompatibilityDeadline(7, now)).toBe(
    new Date(2030, 1, 4, 23, 59, 59, 999).toISOString(),
  )
  expect(modelCompatibilityDeadline(30, now)).toBe(
    new Date(2030, 1, 27, 23, 59, 59, 999).toISOString(),
  )
  expect(modelCompatibilityDeadline(90, now)).toBe(
    new Date(2030, 3, 28, 23, 59, 59, 999).toISOString(),
  )
  expect(now.valueOf()).toBe(before)
})

it('keeps compatibility explicit with only 7/30/90 choices and a stable selected-language deadline', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2030, 0, 1, 12))
  await mount()
  const keep = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!
  expect(keep.checked).toBe(true)
  keep.focus()
  expect(document.activeElement).toBe(keep)
  const select = host.querySelector('select')!
  expect([...select.options].map((option) => option.value)).toEqual(['7', '30', '90'])
  for (const days of [7, 30, 90]) {
    await act(async () => {
      select.value = String(days)
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(expiration()).toBe(new Date(2030, 0, 1 + days, 23, 59, 59, 999).toISOString())
  }
  const captured = expiration()
  vi.setSystemTime(new Date(2030, 0, 5, 12))
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('旧名称继续可用')
  expect(host.textContent).toContain('兼容期限结束后')
  expect(expiration()).toBe(captured)
  expect(host.textContent).toContain(new Date(captured!).toLocaleString('zh-CN'))
})

it('omits the deadline for explicit immediate stop and restores the reviewed deadline when checked', async () => {
  await mount()
  const captured = expiration()
  const keep = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!
  await act(async () => keep.click())
  expect(keep.checked).toBe(false)
  expect(host.querySelector('select')).toBeNull()
  expect(expiration()).toBeUndefined()
  expect(new FormData(host.querySelector('form')!).has('alias_expires_at')).toBe(false)
  expect(host.textContent).toContain('immediately stop accepting new requests using Original')
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('将立即停止')
  await act(async () => keep.click())
  expect(expiration()).toBe(captured)
})

it('renders captured names as text without inventing calls, grants or runtime proof', async () => {
  await mount('<script>Original</script>')
  expect(host.querySelector('script')).toBeNull()
  expect(host.textContent).toContain('<script>Original</script>')
  expect(host.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe(
    '<script>Original</script>',
  )
  expect(host.querySelector('input[name="reason"]')).toBeNull()
  expect(host.querySelector('input[name="etag"]')).toBeNull()
})
