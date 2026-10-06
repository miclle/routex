import type { MultiSelectOption } from '@/components/ui/multi-select'

export interface TeamCreationModelOption extends MultiSelectOption {
  providerNames: readonly string[]
  protocols: readonly string[]
}

export function teamCreationModelLayout(
  options: readonly TeamCreationModelOption[],
  headers: readonly [string, string, string],
) {
  const displayWidth = (value: string) =>
    Array.from(value).reduce((width, char) => width + ((char.codePointAt(0) ?? 0) > 255 ? 2 : 1), 0)
  const rows = options.map((option) => [
    option.label,
    option.providerNames.join(', '),
    option.protocols.join(', '),
  ])
  const widths = headers.map(
    (header, column) =>
      Math.max(displayWidth(header), ...rows.map((row) => displayWidth(row[column]))) * 8 + 16,
  )
  return { gridTemplateColumns: widths.map((width) => `${width}px`).join(' ') }
}
