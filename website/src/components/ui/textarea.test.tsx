import { act, createRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { Textarea } from './textarea'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

it('preserves a standalone textarea ref, label, rows, required form value and native change event', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const ref = createRef<HTMLTextAreaElement>()
  const changed = vi.fn()
  const keydown = vi.fn()
  try {
    await act(async () =>
      root.render(
        <form>
          <label htmlFor="description">Description</label>
          <Textarea
            ref={ref}
            id="description"
            name="description"
            rows={3}
            required
            defaultValue=""
            className="min-h-16"
            onChange={(event) => changed(event.currentTarget)}
            onKeyDown={keydown}
          />
        </form>,
      ),
    )
    const field = container.querySelector('textarea')!
    expect(container.querySelector('input')).toBeNull()
    expect(ref.current).toBe(field)
    expect(field.labels?.[0]?.textContent).toBe('Description')
    expect(field.rows).toBe(3)
    expect(field.className).toContain('min-h-16')
    expect(field.checkValidity()).toBe(false)
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        field,
        'Line one\nLine two',
      )
      field.dispatchEvent(new Event('input', { bubbles: true }))
      field.focus()
    })
    expect(changed).toHaveBeenCalledWith(field)
    expect(field.value).toBe('Line one\nLine two')
    expect(document.activeElement).toBe(field)
    expect(new FormData(container.querySelector('form')!).get('description')).toBe(field.value)
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    await act(async () => field.dispatchEvent(enter))
    expect(keydown).toHaveBeenCalledTimes(1)
    expect(enter.defaultPrevented).toBe(false)
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})

it('preserves controlled multiline values and disabled/read-only native semantics', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const changed = vi.fn()
  function Editor({
    disabled = false,
    readOnly = false,
  }: {
    disabled?: boolean
    readOnly?: boolean
  }) {
    const [value, setValue] = useState('Original')
    return (
      <form>
        <Textarea
          name="notes"
          value={value}
          disabled={disabled}
          readOnly={readOnly}
          onChange={(event) => {
            changed(event.currentTarget.value)
            setValue(event.currentTarget.value)
          }}
        />
      </form>
    )
  }
  try {
    await act(async () => root.render(<Editor />))
    const field = container.querySelector('textarea')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        field,
        'Updated\nNotes',
      )
      field.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(changed).toHaveBeenLastCalledWith('Updated\nNotes')
    expect(field.value).toBe('Updated\nNotes')
    await act(async () => root.render(<Editor disabled />))
    expect(field.disabled).toBe(true)
    expect(new FormData(container.querySelector('form')!).has('notes')).toBe(false)
    await act(async () => root.render(<Editor readOnly />))
    expect(field.disabled).toBe(false)
    expect(field.readOnly).toBe(true)
    expect(new FormData(container.querySelector('form')!).get('notes')).toBe('Updated\nNotes')
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
