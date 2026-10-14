import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import type { KeyDeliveryDescriptor } from '@/types/key-delivery'
import { vaultAgentConfiguration } from '@/lib/key-delivery-config'

export function ApplicationConfiguration({
  descriptor,
  pending,
  onCopy,
}: {
  descriptor: KeyDeliveryDescriptor
  pending: boolean
  onCopy: (text: string) => void
}) {
  const { t } = useTranslation('keyDeliveryOwner')
  const configuration = vaultAgentConfiguration(descriptor)
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-3">
        <CardTitle>{t('applicationConfiguration')}</CardTitle>
        <span className="text-sm text-muted-foreground">{t('vaultAgentJWT')}</span>
      </CardHeader>
      <CardContent className="space-y-3">
        {configuration ? (
          <>
            <Textarea
              aria-label={t('applicationConfiguration')}
              className="h-64 font-mono text-xs"
              value={configuration.text}
              readOnly
              spellCheck={false}
            />
            <p className="break-all text-sm text-muted-foreground">
              {t('jwtFileInstruction', { path: configuration.jwtFile })}
            </p>
            <p className="break-all text-sm text-muted-foreground">
              {t('keyFileInstruction', { path: configuration.keyFile })}
            </p>
            <p className="text-sm text-muted-foreground">{t('configurationBoundary')}</p>
            <div className="flex justify-end">
              <Button
                variant="outline"
                disabled={pending}
                onClick={() => onCopy(configuration.text)}
              >
                {t('copyConfiguration')}
              </Button>
            </div>
          </>
        ) : (
          <p role="status" className="text-sm text-muted-foreground">
            {t('configurationUnavailable')}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
