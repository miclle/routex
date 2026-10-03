import { useTranslation } from 'react-i18next'
import { Page } from '@/components/app/CatalogUI'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useState } from 'react'
import { useSession } from '@/hooks/use-auth'
import { useSearchParams } from 'react-router'
import ChatWorkbench from './chat'
import CompareWorkbench from './compare'

export default function PlaygroundPage() {
  const [searchParams] = useSearchParams()
  const projectId = searchParams.get('project')?.trim() ?? ''
  const initialTeam = searchParams.get('team')?.trim() ?? ''
  const expectedModel = searchParams.get('model')?.trim() ?? ''
  return (
    <PlaygroundContext
      key={`${projectId}:${initialTeam}:${expectedModel}`}
      projectId={projectId}
      initialTeam={initialTeam}
      expectedModel={expectedModel}
    />
  )
}
function PlaygroundContext({
  projectId,
  initialTeam,
  expectedModel,
}: {
  projectId: string
  initialTeam: string
  expectedModel: string
}) {
  const { t } = useTranslation('playground')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const [source, setSource] = useState<'key' | 'team'>(initialTeam ? 'team' : 'key')
  const [team, setTeam] = useState(initialTeam)
  return (
    <Page title="Playground" description={t('description')}>
      <Tabs defaultValue="chat">
        <TabsList aria-label={t('workbench')}>
          <TabsTrigger value="chat">{t('conversation')}</TabsTrigger>
          <TabsTrigger value="compare">{t('comparison')}</TabsTrigger>
        </TabsList>
        <TabsContent value="chat">
          <ChatWorkbench
            key={`chat:${actor}:${source}:${team}:${projectId}`}
            projectId={projectId}
            source={source}
            teamId={team}
            expectedModel={expectedModel}
            onSource={setSource}
            onTeam={setTeam}
          />
        </TabsContent>
        <TabsContent value="compare">
          <CompareWorkbench
            key={`compare:${actor}:${source}:${team}:${projectId}`}
            projectId={projectId}
            source={source}
            teamId={team}
            onSource={setSource}
            onTeam={setTeam}
          />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
