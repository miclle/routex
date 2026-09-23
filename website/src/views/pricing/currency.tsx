import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import { getCurrency, saveCurrency } from '@/api/currency'
import type { CurrencyPage } from '@/types/currency'
import { currencies, type PriceCurrency, type PricePage } from '@/types/pricing'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, ErrorNotice, FormField } from '@/components/app/CatalogUI'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
const key = ['admin', 'pricing-currency']
function normalized(value: PricePage['currency']) {
  return {
    platform_currency: value.platform_currency,
    rates: Object.fromEntries(
      currencies
        .filter((currency) => value.rates[currency])
        .map((currency) => [currency, value.rates[currency]]),
    ),
  }
}
export default function CurrencyPageView() {
  return (
    <PermissionGate permission="prices.read">
      <Currency />
    </PermissionGate>
  )
}
function Currency() {
  const { t } = useTranslation('currency')
  const query = useQuery({ queryKey: key, queryFn: ({ signal }) => getCurrency(signal) })
  return (
    <Page title={t('title')} description={t('direction')}>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {query.data && (
        <CurrencyEditor
          current={query.data}
          reload={async () => {
            const result = await query.refetch()
            return result.isError ? undefined : result.data
          }}
        />
      )}
    </Page>
  )
}
function CurrencyEditor({
  current: incoming,
  reload,
}: {
  current: CurrencyPage
  reload: () => Promise<CurrencyPage | undefined>
}) {
  const { t } = useTranslation('currency')
  const access = usePermissions()
  const session = useSession()
  const cache = useQueryClient()
  const [current, setCurrent] = useState(incoming)
  const [reviewing, setReviewing] = useState(false)
  const submitted = useRef(false)
  const [draft, setDraft] = useState(() => structuredClone(current.currency))
  const [etag, setETag] = useState(current.etag)
  const [notice, setNotice] = useState('')
  const [validation, setValidation] = useState('')
  const [confirm, setConfirm] = useState(false)
  const dirty = JSON.stringify(normalized(draft)) !== JSON.stringify(normalized(current.currency))
  const mutation = useMutation({
    mutationFn: () => saveCurrency(etag, normalized(draft), session.data!.csrf_token),
    onSuccess: (page) => {
      const next = { ...current, etag: page.etag, currency: page.currency }
      setCurrent(next)
      cache.setQueryData(key, next)
      setDraft(structuredClone(page.currency))
      setETag(page.etag)
      setConfirm(false)
      setNotice('saved')
      void cache.invalidateQueries({ queryKey: ['admin', 'prices'] })
      void cache.invalidateQueries({ queryKey: key })
    },
    onError: () => setConfirm(false),
    onSettled: () => {
      submitted.current = false
    },
  })
  const status = axios.isAxiosError(mutation.error) ? mutation.error.response?.status : undefined
  const stale = status === 409 || incoming.etag !== etag
  function validate(event: FormEvent) {
    event.preventDefault()
    if (!access.can('prices.write') || mutation.isPending || reviewing || stale || !dirty) return
    const rates = normalized(draft).rates
    if (
      Object.values(rates).some(
        (rate) => !/^\d{1,18}(\.\d{1,18})?$/.test(rate) || !/[1-9]/.test(rate),
      )
    ) {
      setValidation('invalid')
      return
    }
    if (current.required_currencies.some((currency) => !rates[currency])) {
      setValidation('missing')
      return
    }
    setValidation('')
    setNotice('')
    setConfirm(true)
  }
  async function review() {
    if (reviewing || submitted.current) return
    setReviewing(true)
    try {
      const next = await reload()
      if (!next) return
      setCurrent(next)
      setETag(next.etag)
      mutation.reset()
      setValidation('')
      setNotice('reviewed')
    } finally {
      setReviewing(false)
    }
  }
  return (
    <section className="space-y-6 rounded-lg border p-6">
      <h2 className="text-lg font-semibold">{t('current')}</h2>
      <div className="space-y-2 rounded-md border bg-muted/30 p-4 text-sm">
        <p>{t('direction')}</p>
        <p className="text-muted-foreground">{t('history')}</p>
      </div>
      <p className="text-sm">
        {t('savedCurrency')}: {current.currency.platform_currency}
      </p>
      <form className="space-y-5" onSubmit={validate}>
        <fieldset
          className="space-y-5"
          disabled={!access.can('prices.write') || mutation.isPending || reviewing}
        >
          <FormField label={t('platform')}>
            <select
              className="h-10 w-full max-w-xs rounded-md border bg-background px-3"
              value={draft.platform_currency}
              onChange={(event) => {
                const currency = event.target.value as PriceCurrency
                setDraft({ platform_currency: currency, rates: { [currency]: '1' } })
                setValidation('')
                setNotice('')
              }}
            >
              {currencies.map((currency) => (
                <option key={currency}>{currency}</option>
              ))}
            </select>
          </FormField>
          <p className="text-sm text-muted-foreground">{t('changeHint')}</p>
          <Table aria-label={t('title')}>
            <thead>
              <tr>
                <th>{t('source')}</th>
                <th>{t('conversion')}</th>
                <th>{t('requirement')}</th>
              </tr>
            </thead>
            <tbody>
              {currencies.map((currency) => (
                <tr key={currency}>
                  <td>{currency}</td>
                  <td>
                    <div className="flex items-center gap-3">
                      <Input
                        className="max-w-xs"
                        aria-label={t('rateLabel', { currency })}
                        inputMode="decimal"
                        disabled={currency === draft.platform_currency}
                        value={draft.rates[currency] ?? ''}
                        onChange={(event) => {
                          setDraft({
                            ...draft,
                            rates: { ...draft.rates, [currency]: event.target.value },
                          })
                          setValidation('')
                        }}
                      />
                      <span>{draft.platform_currency}</span>
                    </div>
                  </td>
                  <td>
                    {t(
                      currency === draft.platform_currency
                        ? 'fixed'
                        : current.required_currencies.includes(currency)
                          ? 'required'
                          : 'optional',
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </fieldset>
        {validation && (
          <p role="alert" className="text-sm text-destructive">
            {t(validation)}
          </p>
        )}
        {stale ? (
          <div className="space-y-3">
            <p role="alert">{t('conflict')}</p>
            <Button variant="outline" disabled={reviewing} onClick={() => void review()}>
              {t('reload')}
            </Button>
          </div>
        ) : status === 422 ? (
          <p role="alert">{t('rejected')}</p>
        ) : (
          <ErrorNotice error={mutation.error} />
        )}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        {access.can('prices.write') && (
          <div className="flex gap-3">
            <Button type="submit" disabled={!dirty || mutation.isPending || reviewing || stale}>
              {t('save')}
            </Button>
            <Button
              variant="outline"
              disabled={!dirty || mutation.isPending || reviewing}
              onClick={() => {
                setDraft(structuredClone(current.currency))
                setETag(current.etag)
                setValidation('')
                setNotice('')
                mutation.reset()
              }}
            >
              {t('cancelChanges')}
            </Button>
          </div>
        )}
      </form>
      <Dialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('confirmTitle')}
        description={t('confirmDescription')}
        busy={mutation.isPending}
      >
        <div className="space-y-3 text-sm">
          <p>
            {t('platform')}: {current.currency.platform_currency} → {draft.platform_currency}
          </p>
          {Object.entries(normalized(draft).rates).map(([currency, rate]) => (
            <p key={currency}>
              1 {currency} = {rate} {draft.platform_currency}
            </p>
          ))}
        </div>
        <div className="mt-6 flex justify-end gap-3">
          <Button variant="outline" disabled={mutation.isPending} onClick={() => setConfirm(false)}>
            {t('cancel')}
          </Button>
          <Button
            disabled={mutation.isPending || stale || reviewing || !access.can('prices.write')}
            onClick={() => {
              if (submitted.current || stale || reviewing || !access.can('prices.write')) return
              submitted.current = true
              mutation.mutate()
            }}
          >
            {t('confirm')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
