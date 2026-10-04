import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { Tooltip } from './tooltip'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
it('supports keyboard help, live language changes, Escape and private portal removal', async () => {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  function Example({ enabled = true }: { enabled?: boolean }) {
    const { t } = useTranslation('governance')
    return (
      <Tooltip label={t('memberTeams.tokensHelpLabel')} enabled={enabled}>
        {t('memberTeams.tokensHelp')}
      </Tooltip>
    )
  }
  const wait = (fn: () => void) =>
    vi.waitFor(async () => {
      await act(async () => {})
      fn()
    })
  try {
    await act(async () => {
      await i18n.changeLanguage('en')
      root.render(<Example />)
    })
    const trigger = host.querySelector('button')!
    await act(async () => trigger.focus())
    await wait(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull())
    expect(trigger.getAttribute('aria-describedby')).toBe(
      document.querySelector('[role="tooltip"]')!.id,
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(trigger.getAttribute('aria-label')).toBe('关于成员 Token 用量和配额')
    expect(document.querySelector('[role="tooltip"]')!.textContent).toContain('团队限制同时适用')
    await act(async () =>
      trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await wait(() => expect(document.querySelector('[role="tooltip"]')).toBeNull())
    await act(async () => {
      trigger.blur()
      trigger.focus()
    })
    await wait(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull())
    await act(async () => root.render(<Example enabled={false} />))
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => root.render(<Example />))
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => {
      trigger.blur()
      trigger.focus()
    })
    await wait(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull())
    await act(async () => trigger.click())
    await wait(() => expect(document.querySelector('[role="tooltip"]')).toBeNull())
  } finally {
    await act(async () => {
      root.unmount()
      await i18n.changeLanguage('en')
    })
    host.remove()
  }
})
it('rejects opening from an obsolete authority callback without hiding native semantics', async () => {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  let allowed = false
  try {
    await act(async () =>
      root.render(
        <Tooltip label="Recorded help" canOpen={() => allowed}>
          Private facts
        </Tooltip>,
      ),
    )
    await act(async () => host.querySelector('button')!.focus())
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    allowed = true
    await act(async () => {
      host.querySelector('button')!.blur()
      host.querySelector('button')!.focus()
    })
    await vi.waitFor(async () => {
      await act(async () => {})
      expect(document.querySelector('[role="tooltip"]')).not.toBeNull()
    })
  } finally {
    await act(async () => root.unmount())
    host.remove()
  }
})
