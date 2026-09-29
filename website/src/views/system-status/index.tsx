import { PermissionGate } from '@/components/app/PermissionGate'
import { SystemStatusWorkspace } from './workspace'

export default function SystemStatusPage() {
  return (
    <PermissionGate permission="system.read">
      <SystemStatusWorkspace />
    </PermissionGate>
  )
}
