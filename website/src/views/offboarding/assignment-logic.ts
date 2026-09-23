import type {
  OffboardingAssignments,
  OffboardingInventory,
  OffboardingResource,
} from '@/types/offboarding'

export type Choices = Record<string, { id: string; name: string }[]>
export function needsEmergencySuccessor(resource: OffboardingResource, userId: string) {
  return (
    resource.requires_successor &&
    !resource.people.some((p) => p.user_id !== userId && !p.disabled && p.status === 'active')
  )
}
export function buildAssignments(
  inventory: OffboardingInventory,
  projects: Choices,
  teams: Choices,
  additions: Record<string, string[]>,
  emergency: boolean,
): OffboardingAssignments | null {
  const projectRows = emergency ? [] : inventory.projects.filter((r) => r.status !== 'archived')
  const teamRows = inventory.teams.filter(
    (r) => r.status !== 'archived' && (!emergency || needsEmergencySuccessor(r, inventory.user_id)),
  )
  if (
    projectRows.some((r) => r.requires_successor && !projects[r.id]?.length) ||
    teamRows.some((r) => r.requires_successor && !teams[r.id]?.length)
  )
    return null
  for (const row of teamRows)
    for (const person of teams[row.id] ?? []) {
      const isMember = row.people.some(
        (p) => p.user_id === person.id && !p.disabled && p.status === 'active',
      )
      if (!isMember && !additions[row.id]?.includes(person.id)) return null
    }
  return {
    project_assignments: projectRows
      .filter((r) => projects[r.id]?.length)
      .map((r) => ({ project_id: r.id, manager_user_ids: projects[r.id].map((p) => p.id).sort() })),
    team_assignments: teamRows
      .filter((r) => teams[r.id]?.length)
      .map((r) => ({
        team_id: r.id,
        owner_user_ids: teams[r.id].map((p) => p.id).sort(),
        add_member_user_ids: (additions[r.id] ?? [])
          .filter((id) => teams[r.id].some((p) => p.id === id))
          .sort(),
      })),
  }
}
