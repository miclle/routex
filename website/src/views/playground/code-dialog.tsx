import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Code } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  buildPlaygroundSnippet,
  type SnippetInput,
  type SnippetLanguage,
} from '@/lib/playground-snippet'
export default function CodeDialog({
  request,
  onClose,
}: {
  request: SnippetInput
  onClose: () => void
}) {
  const { t } = useTranslation('playground'),
    [language, setLanguage] = useState<SnippetLanguage>('curl'),
    [notice, setNotice] = useState<'codeCopied' | 'codeCopyFailed' | null>(null)
  let code = ''
  try {
    code = buildPlaygroundSnippet(request, language)
  } catch {
    /* Invalid paths are explained without fabricating a callable request. */
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(code)
      setNotice('codeCopied')
    } catch {
      setNotice('codeCopyFailed')
    }
  }
  return (
    <Dialog
      open
      title={t('requestCode')}
      description={t('codeHelp')}
      width={760}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <div className="space-y-4">
        <Tabs
          value={language}
          onValueChange={(value) => {
            setLanguage(value as SnippetLanguage)
            setNotice(null)
          }}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <TabsList aria-label={t('codeLanguage')}>
              <TabsTrigger value="curl">cURL</TabsTrigger>
              <TabsTrigger value="python">Python</TabsTrigger>
              <TabsTrigger value="javascript">JavaScript</TabsTrigger>
            </TabsList>
            <Button variant="outline" disabled={!code} onClick={() => void copy()}>
              <Code className="size-4" />
              {t('copyCode')}
            </Button>
          </div>
          <TabsContent value={language}>
            {code ? (
              <pre
                aria-label={t('requestCode')}
                className="max-h-[55vh] overflow-auto rounded-lg border bg-muted/30 p-4 text-xs leading-6"
              >
                {code}
              </pre>
            ) : (
              <p role="alert" className="text-sm text-destructive">
                {t('geminiAliasRequired')}
              </p>
            )}
          </TabsContent>
        </Tabs>
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        <div className="flex justify-end">
          <Button variant="outline" onClick={onClose}>
            {t('codeClose')}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
