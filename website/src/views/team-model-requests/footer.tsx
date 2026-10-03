import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import PersonalAccessRequest from '@/views/personal-model-requests/request'
import TeamAccessRequest from './request'

export default function AccessRequestFooter({
  actorID,
  modelID,
  personalGranted,
  visible,
  onBusy,
}: {
  actorID: string
  modelID: string
  personalGranted: boolean
  visible: boolean
  onBusy: (value: boolean) => void
}) {
  const { t } = useTranslation('teamModelRequests')
  const [scope, setScope] = useState('personal')
  const [personalMounted, setPersonalMounted] = useState(!personalGranted)
  const [teamMounted, setTeamMounted] = useState(false)
  const [personalBusy, setPersonalBusy] = useState(false),
    [teamBusy, setTeamBusy] = useState(false),
    [teamLocked, setTeamLocked] = useState(false)
  if (visible && !personalGranted && !personalMounted) setPersonalMounted(true)
  return (
    <div className="space-y-4 border-t pt-5">
      {visible && (
        <label className="block space-y-2 text-sm font-medium">
          <span>{t('scope')}</span>
          <select
            aria-label={t('scope')}
            value={scope}
            disabled={personalBusy || teamBusy || teamLocked}
            onChange={(event) => {
              setScope(event.target.value)
              if (event.target.value === 'team') setTeamMounted(true)
            }}
            className="h-10 w-full rounded-md border bg-background px-3 text-sm"
          >
            <option value="personal">{t('personal')}</option>
            <option value="team">{t('team')}</option>
          </select>
        </label>
      )}
      {scope === 'personal' && personalGranted && visible && (
        <p role="status">{t('personalModelRequests:granted')}</p>
      )}
      {personalMounted && (
        <div hidden={scope !== 'personal'}>
          <PersonalAccessRequest
            actorID={actorID}
            modelID={modelID}
            visible={visible && scope === 'personal' && !personalGranted}
            onBusy={(value) => {
              setPersonalBusy(value)
              onBusy(value || teamBusy)
            }}
          />
        </div>
      )}
      {teamMounted && (
        <div hidden={scope !== 'team'}>
          <TeamAccessRequest
            actorID={actorID}
            modelID={modelID}
            visible={visible && scope === 'team'}
            onLocked={setTeamLocked}
            onBusy={(value) => {
              setTeamBusy(value)
              onBusy(value || personalBusy)
            }}
          />
        </div>
      )}
    </div>
  )
}
