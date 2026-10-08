import { Autocomplete as BaseAutocomplete } from '@base-ui/react/autocomplete'
import { Input } from '@/components/ui/input'
import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

export function Autocomplete({
  label,
  descriptionId,
  value,
  suggestions,
  disabled,
  onValueChange,
  canSelectSuggestion,
}: {
  label: string
  descriptionId?: string
  value: string
  suggestions: readonly string[]
  disabled?: boolean
  onValueChange: (value: string) => void
  canSelectSuggestion?: (value: string) => boolean
}) {
  const { t } = useTranslation('common')
  const dismissLabel = t('close_6c14b')
  const input = useRef<HTMLInputElement | null>(null)
  const popup = useRef<HTMLDivElement | null>(null)
  const localized = useRef(new WeakSet<Element>())
  const [open, setOpen] = useState(false)
  const localizeDismissals = useCallback(() => {
    // Base UI 1.8.0 hardcodes these native sibling labels without a public prop.
    // Keep its dismissal handlers/focus guards intact; touch only our exact pair.
    for (const node of [input.current?.previousElementSibling, popup.current?.nextElementSibling]) {
      if (
        node?.tagName === 'SPAN' &&
        node.getAttribute('role') === 'button' &&
        (node.getAttribute('aria-label') === 'Dismiss' || localized.current.has(node))
      ) {
        localized.current.add(node)
        node.setAttribute('aria-label', dismissLabel)
      }
    }
  }, [dismissLabel])
  const inputRef = useCallback(
    (node: HTMLInputElement | null) => {
      input.current = node
      if (node) localizeDismissals()
    },
    [localizeDismissals],
  )
  const popupRef = useCallback(
    (node: HTMLDivElement | null) => {
      popup.current = node
      if (node) localizeDismissals()
    },
    [localizeDismissals],
  )
  useLayoutEffect(() => {
    localizeDismissals()
  }, [localizeDismissals, open])
  return (
    <BaseAutocomplete.Root
      value={value}
      onOpenChange={setOpen}
      onValueChange={(next, details) => {
        if (details.reason === 'item-press' && canSelectSuggestion?.(next) === false) {
          details.cancel()
          return
        }
        if (details.reason === 'escape-key') {
          details.cancel()
          return
        }
        onValueChange(next)
      }}
      items={suggestions}
      filter={null}
      disabled={disabled}
      openOnInputClick
    >
      <BaseAutocomplete.Input
        ref={inputRef}
        render={<Input />}
        aria-label={label}
        aria-describedby={descriptionId}
        maxLength={128}
      />
      <BaseAutocomplete.Portal>
        <BaseAutocomplete.Positioner sideOffset={4} className="z-50">
          <BaseAutocomplete.Popup
            ref={popupRef}
            className="w-[var(--anchor-width)] rounded-md border bg-popover p-1 text-popover-foreground shadow-md data-[empty]:hidden"
          >
            <BaseAutocomplete.List className="max-h-64 overflow-auto">
              {(name: string) => (
                <BaseAutocomplete.Item
                  key={name}
                  value={name}
                  className="cursor-default rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent"
                >
                  {name}
                </BaseAutocomplete.Item>
              )}
            </BaseAutocomplete.List>
          </BaseAutocomplete.Popup>
        </BaseAutocomplete.Positioner>
      </BaseAutocomplete.Portal>
    </BaseAutocomplete.Root>
  )
}
