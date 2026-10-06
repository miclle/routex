import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import TeamCreationModels from './team-creation-models'
import {
  teamCreationModelLayout,
  type TeamCreationModelOption,
} from './team-creation-models-values'
import type { MultiSelectOption } from '@/components/ui/multi-select'
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
const options: TeamCreationModelOption[] = [
  {
    value: 'mdl_case',
    label: 'Model Alpha',
    providerNames: ['First', 'Second'],
    protocols: ['openai_chat', 'openai_responses'],
  },
  {
    value: 'mdl_Case',
    label: '模型 Beta',
    providerNames: ['Other'],
    protocols: ['anthropic_messages'],
  },
]
const changed = vi.fn()
function Harness({
  items = options,
  authorized = true,
  disabled = false,
}: {
  items?: TeamCreationModelOption[]
  authorized?: boolean
  disabled?: boolean
}) {
  const [selected, setSelected] = useState<MultiSelectOption[]>([])
  const [search, setSearch] = useState('')
  return (
    <TeamCreationModels
      authorized={authorized}
      options={items}
      selected={selected}
      search={search}
      onSearchChange={setSearch}
      onValueChange={(next) => {
        changed(next)
        setSelected(next)
      }}
      disabled={disabled}
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
function inputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
it('reproduces a searchable three-column popup and keeps exact selections outside a bounded page', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  const headers = document.querySelector('.team-creation-model-columns[aria-hidden="true"]')!
  expect(headers.textContent).toBe('ModelProviderProtocol')
  const rows = document.querySelectorAll<HTMLElement>('[role="option"]')
  expect(rows[0].textContent).toContain('First, Second')
  expect(rows[0].textContent).toContain('openai_chat, openai_responses')
  expect(rows[1].textContent).toContain('Other')
  await act(async () => {
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  })
  await vi.waitFor(() =>
    expect(host.querySelector('[aria-label="Remove Model Alpha"]')).not.toBeNull(),
  )
  await act(async () => rows[1].click())
  expect(changed.mock.lastCall?.[0].map((item: MultiSelectOption) => item.value)).toEqual([
    'mdl_case',
    'mdl_Case',
  ])
  await act(async () => root.render(<Harness items={[]} />))
  expect(host.querySelector('[aria-label="Remove Model Alpha"]')).not.toBeNull()
  expect(host.querySelector('[aria-label="Remove 模型 Beta"]')).not.toBeNull()
})
it('clears all explicit grants while preserving literal search and never submitting the containing form', async () => {
  const submit = vi.fn((event) => event.preventDefault())
  await act(async () =>
    root.render(
      <form onSubmit={submit}>
        <Harness />
      </form>,
    ),
  )
  const input = await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  await act(async () => inputValue(input, '%_ literal'))
  await act(async () =>
    host.querySelector<HTMLButtonElement>('[aria-label="Clear Model selection"]')!.click(),
  )
  expect(changed.mock.lastCall?.[0]).toEqual([])
  expect(input.value).toBe('%_ literal')
  expect(host.textContent).toContain('Leaving this empty grants no Model access.')
  expect(submit).not.toHaveBeenCalled()
})
it('preserves literal search and focus on Escape without changing exact selections', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  await act(async () => {
    inputValue(input, 'Model %_')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  expect(input.value).toBe('Model %_')
  expect(input.getAttribute('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(input)
  expect(host.querySelector('[aria-label="Remove Model Alpha"]')).not.toBeNull()
})
it('live Chinese switching updates columns, clear/remove names and no-grants guidance without losing input', async () => {
  await act(async () => root.render(<Harness />))
  const input = await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  await act(async () => inputValue(input, 'literal_%'))
  await act(async () => i18n.changeLanguage('zh'))
  expect(input.getAttribute('aria-label')).toBe('模型访问')
  expect(input.value).toBe('literal_%')
  expect(host.querySelector('[aria-label="移除 Model Alpha"]')).not.toBeNull()
  expect(host.querySelector('[aria-label="清空模型选择"]')).not.toBeNull()
  expect(
    document.querySelector('.team-creation-model-columns[aria-hidden="true"]')?.textContent,
  ).toBe('模型供应商协议')
  expect(host.textContent).toContain('留空不会授予任何模型访问权限。')
  expect(document.querySelectorAll('[aria-label="Dismiss"]')).toHaveLength(0)
})
it('hides the entire private picker when model authority is absent and performs no network reads', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch')
  await act(async () => root.render(<Harness authorized={false} />))
  expect(host.textContent).toBe('')
  expect(host.querySelector('input')).toBeNull()
  expect(document.querySelector('[role="listbox"]')).toBeNull()
  expect(fetch).not.toHaveBeenCalled()
  fetch.mockRestore()
})
it('closes the popup and blocks clear, remove and option changes while fresh authority is pending', async () => {
  await act(async () => root.render(<Harness />))
  await open()
  await act(async () => document.querySelectorAll<HTMLElement>('[role="option"]')[0].click())
  const last = changed.mock.calls.length
  await act(async () => root.render(<Harness disabled />))
  expect(host.querySelector('input')!.disabled).toBe(true)
  expect(document.querySelector('[role="listbox"]')).toBeNull()
  expect(
    host.querySelector('[aria-label="Clear Model selection"]')?.getAttribute('disabled'),
  ).not.toBeNull()
  await act(async () =>
    host.querySelector<HTMLButtonElement>('[aria-label="Clear Model selection"]')!.click(),
  )
  expect(changed.mock.calls).toHaveLength(last)
})
it('renders recorded metadata as text and uses a stable ID as an unavailable selected label', async () => {
  await act(async () =>
    root.render(
      <TeamCreationModels
        authorized
        options={[{ ...options[0], label: '<script>recorded</script>' }]}
        selected={[{ value: 'mdl_missing', label: 'mdl_missing' }]}
        search=""
        onSearchChange={vi.fn()}
        onValueChange={vi.fn()}
      />,
    ),
  )
  expect(host.querySelector('[aria-label="Remove mdl_missing"]')).not.toBeNull()
  const input = host.querySelector('input')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await vi.waitFor(() => expect(document.querySelectorAll('[role="option"]')).toHaveLength(1))
  expect(document.querySelector('[role="option"]')?.textContent).toContain(
    '<script>recorded</script>',
  )
  expect(document.querySelector('[role="option"] script')).toBeNull()
})
it('uses the same measured columns for headers and rows, widening for recorded provider/protocol values', () => {
  const headers: [string, string, string] = ['Model', 'Provider', 'Protocol']
  const empty = teamCreationModelLayout([], headers)
  const recorded = teamCreationModelLayout(options, headers)
  const base = empty.gridTemplateColumns.split(' ').map((v) => Number.parseInt(v))
  const widths = recorded.gridTemplateColumns.split(' ').map((v) => Number.parseInt(v))
  expect(widths.every((value, index) => value >= base[index])).toBe(true)
  expect(widths[2]).toBeGreaterThan(base[2])
})
