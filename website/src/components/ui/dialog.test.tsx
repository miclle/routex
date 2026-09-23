import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it } from 'vitest'
import { Dialog } from './dialog'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
it('labels the modal, focuses a control, and supports Escape dismissal', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  function Example() {
    const [open, setOpen] = useState(true)
    return (
      <Dialog open={open} onOpenChange={setOpen} title="Confirm" description="Review the action">
        <button>Proceed</button>
      </Dialog>
    )
  }
  try {
    await act(async () => {
      root.render(<Example />)
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50))
    })
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog).not.toBeNull()
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent).toBe(
      'Confirm',
    )
    expect(dialog.contains(document.activeElement)).toBe(true)
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await new Promise((r) => setTimeout(r, 30))
    })
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  } finally {
    await act(async () => {
      root.unmount()
    })
    container.remove()
  }
})
