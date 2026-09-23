import { useTranslation } from 'react-i18next'
import { Page } from '@/components/app/CatalogUI'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import ChatWorkbench from './chat'
import CompareWorkbench from './compare'

export default function PlaygroundPage() {
  const { t } = useTranslation('playground')
  return (
    <Page title="Playground" description={t('description')}>
      <Tabs defaultValue="chat">
        <TabsList aria-label={t('workbench')}>
          <TabsTrigger value="chat">{t('conversation')}</TabsTrigger>
          <TabsTrigger value="compare">{t('comparison')}</TabsTrigger>
        </TabsList>
        <TabsContent value="chat">
          <ChatWorkbench />
        </TabsContent>
        <TabsContent value="compare">
          <CompareWorkbench />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
