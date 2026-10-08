type CompatibilityDays = 7 | 30 | 90
export interface ModelRenameDraft {
  name: string
  keepOldName: boolean
  days: CompatibilityDays
  expiresAt: string
}

export function modelCompatibilityDeadline(days: CompatibilityDays, now = new Date()) {
  const deadline = new Date(now)
  deadline.setDate(deadline.getDate() + days)
  deadline.setHours(23, 59, 59, 999)
  return deadline.toISOString()
}

export function initialModelRenameDraft(name: string): ModelRenameDraft {
  return { name, keepOldName: true, days: 30, expiresAt: modelCompatibilityDeadline(30) }
}
