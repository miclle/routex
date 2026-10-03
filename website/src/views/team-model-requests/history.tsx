import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import PersonalRequestPanel from '@/views/personal-model-requests/requests'
import TeamRequestPanel from './requests'

export default function ModelRequestHistory({
  actor,
  visible,
  onBusy,
}: {
  actor: string
  visible: boolean
  onBusy?: (value: boolean) => void
}) {
  const { t } = useTranslation('teamModelRequests')
  const [teamBusy, setTeamBusy] = useState(false)
  const [scope, setScope] = useState('personal'),
    [teamMounted, setTeamMounted] = useState(false)
  return (
    <Tabs
      value={scope}
      onValueChange={(value) => {
        setScope(String(value))
        if (value === 'team') setTeamMounted(true)
      }}
    >
      <TabsList aria-label={t('scope')}>
        <TabsTrigger value="personal" disabled={teamBusy}>
          {t('personal')}
        </TabsTrigger>
        <TabsTrigger value="team" disabled={teamBusy}>
          {t('team')}
        </TabsTrigger>
      </TabsList>
      <TabsContent value="personal" keepMounted>
        <PersonalRequestPanel key={actor} visible={visible && scope === 'personal'} />
      </TabsContent>
      <TabsContent value="team" keepMounted>
        {teamMounted && (
          <TeamRequestPanel
            key={actor}
            visible={visible && scope === 'team'}
            onBusy={(value) => {
              setTeamBusy(value)
              onBusy?.(value)
            }}
          />
        )}
      </TabsContent>
    </Tabs>
  )
}
