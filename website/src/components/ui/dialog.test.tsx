import { act, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { Dialog } from './dialog'
import { Menu, MenuItem } from './menu'

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

it.each(['Escape', 'Complete'])(
  'uses the explicit row ref after native %s closure from a portal menu',
  async (action) => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    function Example() {
      const [open, setOpen] = useState(false)
      const trigger = useRef<HTMLButtonElement | null>(null)
      return (
        <>
          <Menu label="Exact row actions" trigger="Actions" triggerRef={trigger}>
            <MenuItem onClick={() => setOpen(true)}>Review</MenuItem>
          </Menu>
          <Dialog
            open={open}
            onOpenChange={setOpen}
            title="Confirm row"
            description="Review this row"
            finalFocus={trigger}
          >
            <button onClick={() => setOpen(false)}>Complete</button>
          </Dialog>
        </>
      )
    }
    const wait = async (assertion: () => void) =>
      vi.waitFor(async () => {
        await act(async () => {})
        assertion()
      })
    try {
      await act(async () => root.render(<Example />))
      const trigger = container.querySelector<HTMLButtonElement>(
        '[aria-label="Exact row actions"]',
      )!
      await act(async () => {
        trigger.focus()
        trigger.click()
      })
      await wait(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
      await act(async () => (document.querySelector('[role="menuitem"]') as HTMLElement).click())
      await wait(() =>
        expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(
          true,
        ),
      )
      expect(document.querySelector('[role="menu"]')).toBeNull()
      await act(async () => {
        if (action === 'Escape')
          document.activeElement!.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
          )
        else
          Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button'))
            .find((button) => button.textContent === 'Complete')!
            .click()
      })
      await wait(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
      await wait(() => expect(document.activeElement).toBe(trigger))
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  },
)
