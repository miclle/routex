import { Combobox } from '@base-ui/react/combobox'
import { useCallback, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, X } from 'lucide-react'
import { Input } from './input'
import { Button } from './button'

export interface MultiSelectOption {
  value: string
  label: string
}
export function MultiSelect({
  label,
  options,
  value,
  search,
  onSearchChange,
  onValueChange,
  removeLabel,
  disabled = false,
  footer,
}: {
  label: string
  options: MultiSelectOption[]
  value: MultiSelectOption[]
  search: string
  onSearchChange: (search: string) => void
  onValueChange: (value: MultiSelectOption[]) => void
  removeLabel: (label: string) => string
  disabled?: boolean
  footer?: ReactNode
}) {
  const { t } = useTranslation('common')
  const [open, setOpen] = useState(false)
  const [previousDisabled, setPreviousDisabled] = useState(disabled)
  if (previousDisabled !== disabled) {
    setPreviousDisabled(disabled)
    if (disabled) setOpen(false)
  }
  const input = useRef<HTMLInputElement | null>(null),
    popup = useRef<HTMLDivElement | null>(null)
  const localized = useRef(new WeakSet<Element>())
  const closeLabel = t('close_6c14b')
  const localizeDismissals = useCallback(() => {
    // Pinned Base UI exposes no label prop for this native focus-guard pair.
    for (const node of [input.current?.previousElementSibling, popup.current?.nextElementSibling]) {
      if (
        node?.tagName === 'SPAN' &&
        node.getAttribute('role') === 'button' &&
        (node.getAttribute('aria-label') === 'Dismiss' || localized.current.has(node))
      ) {
        localized.current.add(node)
        node.setAttribute('aria-label', closeLabel)
      }
    }
  }, [closeLabel])
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
  }, [localizeDismissals, open, disabled])
  return (
    <Combobox.Root
      multiple
      items={options}
      value={value}
      inputValue={search}
      filter={null}
      itemToStringLabel={(item) => item.label}
      itemToStringValue={(item) => item.value}
      isItemEqualToValue={(item, selected) => item.value === selected.value}
      disabled={disabled}
      open={open && !disabled}
      onOpenChange={setOpen}
      openOnInputClick
      onInputValueChange={(next, details) => {
        if (details.reason === 'escape-key' || details.reason === 'input-clear') {
          details.cancel()
          return
        }
        if (!disabled) onSearchChange(next)
      }}
      onValueChange={(next) => {
        if (!disabled) onValueChange(next)
      }}
    >
      <Combobox.Chips className="flex min-h-11 flex-wrap items-center gap-1 rounded-md border border-input px-2">
        {value.map((option) => (
          <Combobox.Chip
            key={option.value}
            className="flex max-w-full items-center gap-1 rounded border px-2 py-1 text-xs"
          >
            <span className="truncate">{option.label}</span>
            <Combobox.ChipRemove
              aria-label={removeLabel(option.label)}
              disabled={disabled}
              className="rounded-sm outline-none focus-visible:ring-2"
            >
              <X className="size-3" aria-hidden="true" />
            </Combobox.ChipRemove>
          </Combobox.Chip>
        ))}
        <Combobox.Input
          ref={inputRef}
          render={<Input className="min-w-32 flex-1 border-0 px-1 focus-visible:ring-0" />}
          aria-label={label}
          maxLength={200}
        />
        <Combobox.Trigger render={<Button variant="ghost" size="icon" />} aria-label={label}>
          <ChevronDown className="size-4" aria-hidden="true" />
        </Combobox.Trigger>
      </Combobox.Chips>
      {!disabled && (
        <Combobox.Portal>
          <Combobox.Positioner sideOffset={4} className="z-50">
            <Combobox.Popup
              ref={popupRef}
              className="w-[var(--anchor-width)] min-w-60 rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
            >
              <Combobox.List className="max-h-56 overflow-auto">
                {(option: MultiSelectOption) => (
                  <Combobox.Item
                    key={option.value}
                    value={option}
                    className="flex cursor-default items-center gap-2 rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent"
                  >
                    <Combobox.ItemIndicator>
                      <Check className="size-4" aria-hidden="true" />
                    </Combobox.ItemIndicator>
                    <span className="min-w-0 truncate">{option.label}</span>
                  </Combobox.Item>
                )}
              </Combobox.List>
              {footer}
            </Combobox.Popup>
          </Combobox.Positioner>
        </Combobox.Portal>
      )}
    </Combobox.Root>
  )
}
