import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Bell } from 'lucide-react'
import { useSession } from '@/hooks/use-auth'
import { getAnnouncements } from '@/api/site'
import { Button } from '@/components/ui/button'
export function AnnouncementFeed() {
  const { t } = useTranslation('announcements')
  const session = useSession()
  const query = useQuery({
    queryKey: ['announcements', session.data?.user.id, 'active'],
    queryFn: ({ signal }) => getAnnouncements(false, undefined, signal),
    enabled: !!session.data,
    retry: false,
    refetchInterval: 60_000,
  })
  if (!session.data || query.isPending) return null
  if (query.isError)
    return (
      <div
        role="status"
        className="mb-4 flex flex-wrap items-center gap-3 text-sm text-muted-foreground"
      >
        <span>{t('feedError')}</span>
        <Button variant="ghost" onClick={() => void query.refetch()}>
          {t('retry')}
        </Button>
      </div>
    )
  if (!query.data.items.length) return null
  return (
    <section aria-label={t('feed')} className="mb-6 rounded-lg border bg-muted/20 p-4">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-medium">
        <Bell className="size-4" aria-hidden="true" />
        {t('feed')}
      </h2>
      <ul className="space-y-3">
        {query.data.items
          .filter((item) => item.status === 'active')
          .map((item) => (
            <li key={item.id} className="whitespace-pre-wrap break-words text-sm leading-6">
              {item.content}
            </li>
          ))}
      </ul>
    </section>
  )
}
