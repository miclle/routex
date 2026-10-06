import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it } from 'vitest'
import { Drawer } from './drawer'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
it('keeps a pending drawer open and restores dismissal once the write completes', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  function Example() {
    const [open, setOpen] = useState(true)
    const [busy, setBusy] = useState(true)
    return (
      <Drawer open={open} onOpenChange={setOpen} busy={busy} title="Details">
        <button onClick={() => setBusy(false)}>Finish</button>
      </Drawer>
    )
  }
  try {
    await act(async () => {
      root.render(<Example />)
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    const dialog = document.querySelector('[role="dialog"]')!
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent).toBe(
      'Details',
    )
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => {
      ;[...dialog.querySelectorAll('button')]
        .find((button) => button.textContent === 'Finish')!
        .click()
    })
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  } finally {
    await act(async () => {
      root.unmount()
    })
    container.remove()
  }
})

it('forwards an explicit current final-focus target through conditional unmount', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  let destination: HTMLButtonElement | null = null
  function Example() {
    const [open, setOpen] = useState(false)
    return (
      <>
        <button
          ref={(node) => {
            destination = node
          }}
          onClick={() => setOpen(true)}
        >
          Open current
        </button>
        {open && (
          <Drawer
            open
            onOpenChange={setOpen}
            title="Explicit focus"
            finalFocus={() => destination ?? false}
          >
            Content
          </Drawer>
        )}
      </>
    )
  }
  try {
    await act(async () => root.render(<Example />))
    await act(async () => destination!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 30)))
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog).not.toBeNull()
    await act(async () => dialog.querySelector<HTMLButtonElement>('button')!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 30)))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(destination)
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
