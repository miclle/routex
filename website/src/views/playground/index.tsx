import { useTranslation } from 'react-i18next'
import { Page } from '@/components/app/CatalogUI'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useSearchParams } from 'react-router'
import ChatWorkbench from './chat'
import CompareWorkbench from './compare'

export default function PlaygroundPage() {
  const { t } = useTranslation('playground')
  const [searchParams] = useSearchParams()
  const projectId = searchParams.get('project')?.trim() ?? ''
  return (
    <Page title="Playground" description={t('description')}>
      <Tabs defaultValue="chat">
        <TabsList aria-label={t('workbench')}>
          <TabsTrigger value="chat">{t('conversation')}</TabsTrigger>
          <TabsTrigger value="compare">{t('comparison')}</TabsTrigger>
        </TabsList>
        <TabsContent value="chat">
          <ChatWorkbench key={`chat:${projectId}`} projectId={projectId} />
        </TabsContent>
        <TabsContent value="compare">
          <CompareWorkbench key={`compare:${projectId}`} projectId={projectId} />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
