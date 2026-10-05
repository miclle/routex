import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { Input } from './input'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

it('keeps native label, password, form submission, and disabled semantics through Base UI', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const changed = vi.fn()
  try {
    await act(async () => {
      root.render(
        <form>
          <label htmlFor="secret">Password</label>
          <Input id="secret" name="secret" type="password" required onValueChange={changed} />
        </form>,
      )
    })
    const input = container.querySelector('input')!
    expect(input.labels?.[0]?.textContent).toBe('Password')
    expect(input.type).toBe('password')
    expect(input.checkValidity()).toBe(false)
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'secret-value',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.focus()
    })
    expect(changed).toHaveBeenCalledWith('secret-value', expect.anything())
    expect(document.activeElement).toBe(input)
    expect(new FormData(container.querySelector('form')!).get('secret')).toBe('secret-value')
    await act(async () => {
      root.render(<Input disabled aria-label="Password" />)
    })
    expect(container.querySelector('input')!.disabled).toBe(true)
  } finally {
    await act(async () => {
      root.unmount()
    })
    container.remove()
  }
})

it('preserves compact checkbox labels, keyboard events, controlled state, submission and disabled input', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const changed = vi.fn()
  const keydown = vi.fn()
  function Permission({ disabled = false }: { disabled?: boolean }) {
    const [checked, setChecked] = useState(false)
    return (
      <form>
        <label htmlFor="permission">Read providers</label>
        <Input
          id="permission"
          type="checkbox"
          name="permissions"
          value="providers.read"
          className="h-4 w-4 shrink-0 rounded border-input p-0 accent-primary"
          checked={checked}
          disabled={disabled}
          onKeyDown={keydown}
          onChange={(event) => {
            changed(event.currentTarget.checked)
            setChecked(event.currentTarget.checked)
          }}
        />
      </form>
    )
  }
  try {
    await act(async () => root.render(<Permission />))
    const input = container.querySelector('input')!
    const label = container.querySelector('label')!
    const form = container.querySelector('form')!
    expect(input.type).toBe('checkbox')
    expect(input.labels?.[0]).toBe(label)
    expect(input.checked).toBe(false)
    expect(input.className).toContain('h-4')
    expect(input.className).toContain('w-4')
    await act(async () => input.focus())
    expect(document.activeElement).toBe(input)
    const space = new KeyboardEvent('keydown', {
      key: ' ',
      code: 'Space',
      bubbles: true,
      cancelable: true,
    })
    await act(async () => input.dispatchEvent(space))
    expect(keydown).toHaveBeenCalledTimes(1)
    expect(space.defaultPrevented).toBe(false)
    // jsdom does not implement keyboard default activation; retain that browser-owned action.
    await act(async () => label.click())
    expect(input.checked).toBe(true)
    expect(changed).toHaveBeenLastCalledWith(true)
    expect(new FormData(form).getAll('permissions')).toEqual(['providers.read'])
    await act(async () => input.click())
    expect(input.checked).toBe(false)
    expect(changed).toHaveBeenLastCalledWith(false)
    expect(new FormData(form).getAll('permissions')).toEqual([])
    await act(async () => root.render(<Permission disabled />))
    expect(input.disabled).toBe(true)
    expect(input.checked).toBe(false)
    const changeCount = changed.mock.calls.length
    await act(async () => {
      label.click()
      input.click()
    })
    expect(changed).toHaveBeenCalledTimes(changeCount)
    expect(input.checked).toBe(false)
    expect(new FormData(form).getAll('permissions')).toEqual([])
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
