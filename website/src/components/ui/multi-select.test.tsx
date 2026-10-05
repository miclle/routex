import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MultiSelect, type MultiSelectOption } from './multi-select'
import i18n from '@/i18n'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})
const options = [
  { value: 'one', label: 'One' },
  { value: 'two', label: 'Two' },
]
function Harness({
  disabled = false,
  items = options,
}: {
  disabled?: boolean
  items?: MultiSelectOption[]
}) {
  const [value, setValue] = useState<MultiSelectOption[]>([]),
    [search, setSearch] = useState('')
  return (
    <MultiSelect
      label="Choose roles"
      options={items}
      value={value}
      search={search}
      onSearchChange={setSearch}
      onValueChange={setValue}
      disabled={disabled}
      removeLabel={(label) => `Remove ${label}`}
    />
  )
}
async function open() {
  const input = host.querySelector('input')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await vi.waitFor(() => expect(document.querySelectorAll('[role="option"]')).toHaveLength(2))
  return input
}
it('selects multiple exact options through keyboard and pointer, preserving selections outside a later page', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  await act(async () => {
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  })
  await vi.waitFor(() => expect(host.querySelector('[aria-label="Remove One"]')).not.toBeNull())
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[1].click())
  expect(host.querySelector('[aria-label="Remove Two"]')).not.toBeNull()
  await act(async () => root.render(<Harness items={[]} />))
  expect(host.textContent).toContain('One')
  expect(host.textContent).toContain('Two')
})
it('preserves literal search on Escape and dismisses popup without changing selections', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      '%_ literal',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  expect(input.value).toBe('%_ literal')
  expect(input.getAttribute('aria-expanded')).toBe('false')
})
it('immediately closes private popup and disables chip controls during renewed authority', async () => {
  await act(async () => root.render(<Harness />))
  await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  await act(async () => root.render(<Harness disabled />))
  expect(host.querySelector('input')!.disabled).toBe(true)
  expect(host.querySelector('[aria-label="Remove One"]')?.getAttribute('aria-disabled')).toBe(
    'true',
  )
  expect(host.querySelector('input')!.getAttribute('aria-expanded')).toBe('false')
  expect(document.querySelector('[role="listbox"]')).toBeNull()
})
it.each([
  ['en', 'Close'],
  ['zh', '关闭'],
])('localizes both native focus-guard dismiss controls in %s', async (lang, label) => {
  await act(async () => i18n.changeLanguage(lang))
  await act(async () => root.render(<Harness />))
  const input = await open()
  const list = document.getElementById(input.getAttribute('aria-controls')!)!,
    popup = list.parentElement!
  const guards = [input.previousElementSibling!, popup.nextElementSibling!]
  for (const guard of guards) {
    expect(guard.tagName).toBe('SPAN')
    expect(guard.getAttribute('role')).toBe('button')
    expect(guard.getAttribute('aria-label')).toBe(label)
  }
  expect(document.querySelectorAll('[aria-label="Dismiss"]')).toHaveLength(0)
})
it('live bilingual dismissal update leaves controlled selections and literal search unchanged', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.querySelector('[aria-label="Remove One"]')).not.toBeNull()
  expect(document.querySelectorAll('[aria-label="Dismiss"]')).toHaveLength(0)
  expect(input.value).toBe('')
})
