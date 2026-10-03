import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import type { DefaultLimitPolicy } from '@/types/default-limits'

import { sections, type PolicyDraft } from './policy-values'
export function PolicyRows({
  policy,
  draft,
  change,
  currency,
}: {
  policy: DefaultLimitPolicy
  draft?: PolicyDraft
  change?: (field: keyof PolicyDraft, value: string) => void
  currency: string
}) {
  const { t, i18n } = useTranslation('defaultLimits')
  return (
    <div className="space-y-8">
      {sections.map((section) => (
        <section key={section.key} aria-label={t(section.key)}>
          <h3 className="border-b pb-3 font-medium">{t(section.key)}</h3>
          {section.fields.map((field) => (
            <div
              key={field}
              className="grid gap-4 border-b py-5 sm:grid-cols-[5fr_7fr] sm:items-center"
            >
              <div>
                <label htmlFor={draft ? `default-${field}` : undefined} className="font-medium">
                  {t(field)}
                </label>
                <p className="mt-1 text-sm text-muted-foreground">
                  {t(field === 'money_month' ? 'moneyHelp' : `${field}Help`)}
                </p>
              </div>
              <div>
                {draft ? (
                  <Input
                    id={`default-${field}`}
                    aria-label={t(field)}
                    inputMode={field === 'money_month' ? 'decimal' : 'numeric'}
                    value={draft[field]}
                    placeholder={t('unlimited')}
                    onValueChange={(value) => change?.(field, value)}
                  />
                ) : (
                  <p className="font-semibold">
                    {policy[field] === null
                      ? t('unlimited')
                      : field === 'money_month'
                        ? `${policy[field]} ${policy.currency}`
                        : Number(policy[field]).toLocaleString(i18n.resolvedLanguage)}
                  </p>
                )}
                {field === 'money_month' && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t('currency', { currency })}
                  </p>
                )}
              </div>
            </div>
          ))}
        </section>
      ))}
    </div>
  )
}
