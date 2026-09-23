import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'
import type { UsageGroup } from '@/types/usage'
import { TokenValue, AmountValues } from './stats'

export default function UsageKeyTable({ groups }: { groups: UsageGroup[] }) {
  const { t } = useTranslation('usage')
  const [page, setPage] = useState(0)
  const pages = Math.max(1, Math.ceil(groups.length / 20))
  const current = Math.min(page, pages - 1)
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('keyRanking')}</CardTitle>
      </CardHeader>
      {!groups.length ? (
        <p className="p-6 text-sm text-muted-foreground">{t('noCalls')}</p>
      ) : (
        <>
          <Table aria-label={t('keyRanking')}>
            <thead>
              <tr>
                <th>{t('key')}</th>
                <th>{t('requests')}</th>
                <th>{t('totalTokens')}</th>
                <th>{t('coverage')}</th>
                <th>{t('charges')}</th>
              </tr>
            </thead>
            <tbody>
              {groups.slice(current * 20, (current + 1) * 20).map((group) => (
                <tr key={group.id}>
                  <td className="break-all font-mono text-xs">
                    {group.id || t('unknownIdentity')}
                  </td>
                  <td>{group.stats.requests}</td>
                  <td>
                    <TokenValue value={group.stats.tokens.total} compact />
                  </td>
                  <td className="text-xs text-muted-foreground">
                    {t('missing', { count: group.stats.tokens.total.unknown_calls })}
                  </td>
                  <td>
                    <AmountValues amounts={group.stats.amounts} />
                    {group.stats.unknown_amount_calls > 0 && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {t('unknownCharges', { count: group.stats.unknown_amount_calls })}
                      </p>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
          {pages > 1 && (
            <div className="flex items-center justify-end gap-3 p-3 text-xs text-muted-foreground">
              <span>{t('page', { page: current + 1, pages })}</span>
              <Button
                variant="outline"
                size="sm"
                disabled={current === 0}
                onClick={() => setPage(current - 1)}
              >
                {t('previousPage')}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={current === pages - 1}
                onClick={() => setPage(current + 1)}
              >
                {t('nextPage')}
              </Button>
            </div>
          )}
        </>
      )}
    </Card>
  )
}
