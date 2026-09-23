import ResourceLimits from '@/views/resource-limits'
import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useSession } from '@/hooks/use-auth'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  useTranslation()

  const { data: session } = useSession()
  if (!session) return null
  return (
    <section className="space-y-6">
      <h1 className="sr-only">
        {t('hello_ce0e9')}
        {session.user.name}
      </h1>
      <section
        aria-label={t('value_member_information_17015', { v0: session.user.name })}
        className="flex flex-wrap items-center gap-4 rounded-lg border p-3"
      >
        <span className="flex size-12 items-center justify-center rounded-full bg-muted">
          {session.user.name.slice(0, 2).toUpperCase()}
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[30px] leading-[38px] font-semibold">{session.user.name}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{session.user.email}</p>
          <p className="text-sm leading-6 text-muted-foreground">
            {t('role_908a9')}
            {session.user.role === 'admin' ? t('administrator_ef84e') : t('memberRole')}
          </p>
        </div>
        <Badge variant="outline">{t('active_f78d0')}</Badge>
      </section>
      <ResourceLimits path={`/admin/members/${session.user.id}`} canEdit={false} />
    </section>
  )
}
