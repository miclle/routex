import { useContext, useEffect } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ReauthenticationContext } from './reauthentication-state'
import { Button } from '@/components/ui/button'
import { LanguageSwitcher } from '@/components/app/LanguageSwitcher'
import { SiteLogo, SiteFooter } from '@/components/app/SiteBranding'
export default function ReauthenticationPage() {
  const context = useContext(ReauthenticationContext),
    navigate = useNavigate(),
    { t } = useTranslation('authEnforcement')
  const put = context?.put
  useEffect(() => () => put?.(null), [put])
  // The provider owns the bounded expiry timer and clears the receipt on expiry.
  if (!context?.receipt) return <Navigate to="/login" replace />
  const receipt = context.receipt
  return (
    <main className="flex min-h-screen items-center justify-center p-6">
      <div className="absolute right-6 top-6">
        <LanguageSwitcher />
      </div>
      <section className="w-full max-w-[500px] space-y-6">
        <div className="flex justify-center">
          <SiteLogo />
        </div>
        <div className="space-y-6 rounded-lg border p-6">
          <h1 className="text-[30px] font-semibold leading-[38px]">{t('receipt')}</h1>
          <p role="status">{t('reauthenticate')}</p>
          {receipt.runtimeApplied === false && <p>{t('runtimePending')}</p>}
          {receipt.recoveryCodes.length > 0 && (
            <>
              <p>{t('recoveryReauthenticate')}</p>
              <ul className="space-y-2">
                {receipt.recoveryCodes.map((code) => (
                  <li key={code} className="break-all font-mono text-sm">
                    {code}
                  </li>
                ))}
              </ul>
            </>
          )}
          <Button
            className="w-full"
            onClick={() => {
              context.put(null)
              navigate('/login', { replace: true })
            }}
          >
            {t('signIn')}
          </Button>
        </div>
        <SiteFooter />
      </section>
    </main>
  )
}
