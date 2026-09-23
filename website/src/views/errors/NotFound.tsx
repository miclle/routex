import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router'
import { buttonVariants } from '@/components/ui/button'

// NotFound is the 404 error page.
function NotFound() {
  useTranslation()

  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center space-y-4">
        <h1 className="text-6xl font-bold text-foreground">404</h1>
        <p className="text-muted-foreground">{t('page_not_found_55c9e')}</p>
        <NavLink to="/" className={buttonVariants({ variant: 'outline' })}>
          {t('return_home_8befa')}
        </NavLink>
      </div>
    </div>
  )
}

export default NotFound
