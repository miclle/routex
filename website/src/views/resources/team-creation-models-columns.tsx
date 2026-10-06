import type { CSSProperties } from 'react'

export default function TeamCreationModelColumns({
  values,
  layout,
  header = false,
}: {
  values: readonly [string, string, string]
  layout: CSSProperties
  header?: boolean
}) {
  return (
    <div
      aria-hidden={header || undefined}
      className={`team-creation-model-columns grid w-max gap-6 whitespace-nowrap ${header ? 'font-medium text-muted-foreground' : ''}`}
      style={layout}
    >
      {values.map((value, column) => (
        <span key={column}>{value}</span>
      ))}
    </div>
  )
}
