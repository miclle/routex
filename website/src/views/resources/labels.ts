import { t } from '@/i18n'
import type { ResourceKind, ResourceStatus } from '@/types/resources'
export const resourceLabels: Record<ResourceKind, string> = {
  get teams() {
    return t('teams_21d70')
  },
  get projects() {
    return t('projects_22336')
  },
}
export const statusLabels: Record<ResourceStatus, string> = {
  get active() {
    return t('active_f78d0')
  },
  get disabled() {
    return t('disabled_6c7dc')
  },
  get archived() {
    return t('archived_5cfbe')
  },
}
