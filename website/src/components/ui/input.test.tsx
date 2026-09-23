import { act } from 'react'
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
    await act(async () => { root.render(<form><label htmlFor="secret">密码</label><Input id="secret" name="secret" type="password" required onValueChange={changed} /></form>) })
    const input = container.querySelector('input')!
    expect(input.labels?.[0]?.textContent).toBe('密码')
    expect(input.type).toBe('password')
    expect(input.checkValidity()).toBe(false)
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'secret-value')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.focus()
    })
    expect(changed).toHaveBeenCalledWith('secret-value', expect.anything())
    expect(document.activeElement).toBe(input)
    expect(new FormData(container.querySelector('form')!).get('secret')).toBe('secret-value')
    await act(async () => { root.render(<Input disabled aria-label="密码" />) })
    expect(container.querySelector('input')!.disabled).toBe(true)
  } finally {
    await act(async () => { root.unmount() })
    container.remove()
  }
})
