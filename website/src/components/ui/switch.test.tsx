import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it } from 'vitest'
import { Switch } from './switch'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
it('exposes switch state and submits only checked values', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        <form>
          <Switch name="enabled" aria-label="Registration" />
        </form>,
      ),
    )
    const control = container.querySelector<HTMLElement>('[role="switch"]')!
    expect(control.getAttribute('aria-checked')).toBe('false')
    expect(new FormData(container.querySelector('form')!).get('enabled')).toBeNull()
    await act(async () => control.click())
    expect(control.getAttribute('aria-checked')).toBe('true')
    expect(new FormData(container.querySelector('form')!).get('enabled')).toBe('on')
    await act(async () =>
      root.render(
        <form>
          <Switch name="enabled" aria-label="Registration" disabled />
        </form>,
      ),
    )
    await act(async () => control.click())
    expect(control.getAttribute('aria-checked')).toBe('true')
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
