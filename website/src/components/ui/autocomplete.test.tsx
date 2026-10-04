import { act, StrictMode, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { Autocomplete } from './autocomplete'
import i18n from '@/i18n'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root
beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})
function Harness({
  suggestions = ['gpt-5.2', 'gpt-5.2-2025-12-11'],
  disabled = false,
}: {
  suggestions?: string[]
  disabled?: boolean
}) {
  const [value, setValue] = useState('')
  return (
    <Autocomplete
      label="Public name"
      value={value}
      suggestions={suggestions}
      disabled={disabled}
      onValueChange={setValue}
    />
  )
}
async function type(value: string) {
  const input = host.querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  return input
}
it('preserves custom exact text with no reference suggestions', async () => {
  await act(async () => root.render(<Harness suggestions={[]} />))
  await type('Custom/Model:exact')
  expect(host.querySelector('input')!.value).toBe('Custom/Model:exact')
  expect(host.querySelector('input')!.getAttribute('role')).toBe('combobox')
  expect(document.querySelectorAll('[role="option"]')).toHaveLength(0)
})
it('requires explicit keyboard selection and Escape preserves input', async () => {
  await act(async () => root.render(<Harness />))
  const input = host.querySelector('input')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  expect(document.querySelectorAll('[role="option"]')).toHaveLength(2)
  expect(input.value).toBe('')
  await act(async () =>
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })),
  )
  expect(input.value).toBe('')
  await act(async () =>
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  )
  expect(input.value).toBe('gpt-5.2')
  await type('custom-exact')
  await act(async () =>
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  expect(input.value).toBe('custom-exact')
})
it('uses exact pointer selection and disabled input cannot reopen the list', async () => {
  await act(async () => root.render(<Harness />))
  await act(async () =>
    host
      .querySelector('input')!
      .dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
  )
  const option = document.querySelectorAll<HTMLElement>('[role="option"]')[1]
  await act(async () => option.click())
  expect(host.querySelector('input')!.value).toBe('gpt-5.2-2025-12-11')
  await act(async () => root.render(<Harness disabled />))
  expect(host.querySelector('input')!.disabled).toBe(true)
})

async function openSuggestions(input = host.querySelector('input')!) {
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  expect(input.getAttribute('aria-expanded')).toBe('true')
  return input
}
function nativeDismissals(input: HTMLInputElement) {
  const listId = input.getAttribute('aria-controls')!
  const list = document.getElementById(listId)!
  expect(list.getAttribute('role')).toBe('listbox')
  const popup = list.parentElement!
  const start = input.previousElementSibling!
  const end = popup.nextElementSibling!
  for (const node of [start, end]) {
    // Deliberately assert the pinned library structure: do not silently omit a guard.
    expect(node.tagName).toBe('SPAN')
    expect(node.getAttribute('role')).toBe('button')
    expect(node.getAttribute('aria-hidden')).not.toBe('true')
  }
  return [start, end] as const
}
it.each([
  ['en', 'Close'],
  ['zh', '关闭'],
])('localizes both native open-popup dismiss controls in %s', async (language, label) => {
  await act(async () => i18n.changeLanguage(language))
  await act(async () => root.render(<Harness />))
  const input = await openSuggestions()
  expect(nativeDismissals(input).map((node) => node.getAttribute('aria-label'))).toEqual([
    label,
    label,
  ])
  expect(document.querySelectorAll('[aria-label="Dismiss"]')).toHaveLength(0)
})
it('updates an already open popup on language switch and after close/reopen without changing a custom draft', async () => {
  await act(async () => root.render(<Harness />))
  const input = await type('Custom/exact')
  await openSuggestions(input)
  const original = nativeDismissals(input)
  await act(async () => i18n.changeLanguage('zh'))
  expect(nativeDismissals(input)).toEqual(original)
  expect(original.map((node) => node.getAttribute('aria-label'))).toEqual(['关闭', '关闭'])
  expect(input.value).toBe('Custom/exact')
  await act(async () =>
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  expect(input.getAttribute('aria-expanded')).toBe('false')
  expect(input.value).toBe('Custom/exact')
  await act(async () => i18n.changeLanguage('en'))
  await openSuggestions(input)
  expect(nativeDismissals(input).map((node) => node.getAttribute('aria-label'))).toEqual([
    'Close',
    'Close',
  ])
  expect(input.value).toBe('Custom/exact')
})
it.each([0, 1])('retains native dismissal behavior for guard %s', async (index) => {
  await act(async () => root.render(<Harness />))
  const input = await type('Custom/native-dismiss')
  await openSuggestions(input)
  const guards = nativeDismissals(input)
  await act(async () => (guards[index] as HTMLElement).click())
  expect(input.getAttribute('aria-expanded')).toBe('false')
  expect(input.value).toBe('Custom/native-dismiss')
  await openSuggestions(input)
  expect(nativeDismissals(input).map((node) => node.getAttribute('aria-label'))).toEqual([
    'Close',
    'Close',
  ])
})
it('keeps localization scoped to each mounted row and obsolete controls do not affect a replacement row', async () => {
  await act(async () =>
    root.render(
      <>
        <Harness key="first" />
        <Harness key="second" />
      </>,
    ),
  )
  const inputs = Array.from(host.querySelectorAll<HTMLInputElement>('input[role="combobox"]'))
  await openSuggestions(inputs[0])
  const oldGuards = nativeDismissals(inputs[0])
  await act(async () => oldGuards[1].dispatchEvent(new MouseEvent('click', { bubbles: true })))
  await openSuggestions(inputs[1])
  await act(async () => i18n.changeLanguage('zh'))
  expect(nativeDismissals(inputs[1]).map((node) => node.getAttribute('aria-label'))).toEqual([
    '关闭',
    '关闭',
  ])
  await act(async () => root.render(<Harness key="replacement" />))
  const replacement = await openSuggestions()
  const currentGuards = nativeDismissals(replacement)
  await act(async () => (oldGuards[0] as HTMLElement).click())
  expect(replacement.getAttribute('aria-expanded')).toBe('true')
  expect(nativeDismissals(replacement)).toEqual(currentGuards)
  expect(currentGuards.map((node) => node.getAttribute('aria-label'))).toEqual(['关闭', '关闭'])
})

it('keeps both native labels localized through StrictMode mount and a replacement popup', async () => {
  await act(async () => i18n.changeLanguage('zh'))
  await act(async () =>
    root.render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    ),
  )
  const input = await openSuggestions()
  expect(nativeDismissals(input).map((node) => node.getAttribute('aria-label'))).toEqual([
    '关闭',
    '关闭',
  ])
  await act(async () =>
    root.render(
      <StrictMode>
        <Harness key="new" />
      </StrictMode>,
    ),
  )
  const replacement = await openSuggestions()
  expect(nativeDismissals(replacement).map((node) => node.getAttribute('aria-label'))).toEqual([
    '关闭',
    '关闭',
  ])
})
