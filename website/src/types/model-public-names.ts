export interface ModelPublicNameReference {
  readonly aliases: readonly string[]
  readonly protocols: readonly string[]
  readonly capabilities: readonly string[]
  readonly source_url: string
  readonly reviewed_at: string
}
export interface PublicModelReference extends ModelPublicNameReference {
  readonly name: string
}
export interface ModelPublicNames {
  connection_id: string
  query: string
  items: { name: string; available: boolean; reference: ModelPublicNameReference }[]
}
