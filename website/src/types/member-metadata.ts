export interface MemberMetadataRecord {
  user_id: string
  name: string
  status: 'active' | 'disabled' | 'offboarded'
  can_edit: boolean
  etag: string
}
export interface MemberMetadataInput {
  name: string
  reason: string
}
export interface MemberMetadataWriteResult extends MemberMetadataRecord {
  can_edit: true
  confirmation: 'current_member_name'
}
