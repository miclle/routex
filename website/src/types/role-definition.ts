export interface RoleDefinition {
  id: string
  name: string
  description: string
  builtin: boolean
  permissions: string[]
  available_permissions: string[]
  definition_etag: string
  identity_etag: string | null
  review_etag: string
  can_edit: boolean
}

export interface RoleDefinitionInput {
  name: string
  description: string
  permissions: string[]
  identity_etag: string
  reason: string
}

export interface RoleDefinitionResult {
  id: string
  name: string
  description: string
  permissions: string[]
  identity_etag: string
  etag: string
  confirmation: 'current_role_definition'
  effect: 'current_database'
}
