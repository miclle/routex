import { useQuery } from '@tanstack/react-query'
import { listModels } from '@/api/catalog'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'

export default function ModelsPage() {
  const models = useQuery({ queryKey: ['models'], queryFn: listModels })
  return <Page title="我的模型" description="这里展示已明确授权给你的有效模型。创建个人 Key 时可在此范围内选择。"><QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.data?.length === 0} /><div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{models.data?.map((model) => <article key={model.id} className="space-y-3 rounded-lg border bg-card p-5"><h2 className="break-all font-semibold">{model.name}</h2><Badge variant="outline">{model.protocol === 'openai_chat' ? 'OpenAI Chat' : model.protocol}</Badge><p className="text-xs leading-5 text-muted-foreground">调用时使用此模型名称；实际可用性取决于有效接入与凭证。</p></article>)}</div></Page>
}
