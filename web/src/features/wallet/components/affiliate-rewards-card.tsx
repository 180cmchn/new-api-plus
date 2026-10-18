/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import type { ReferralSummary } from '../api'

interface AffiliateRewardsCardProps {
  summary?: ReferralSummary
  affiliateLink: string
  loading?: boolean
  error?: boolean
}

export function AffiliateRewardsCard({
  summary,
  affiliateLink,
  loading,
  error,
}: AffiliateRewardsCardProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-4 p-3 sm:p-4 lg:grid-cols-3 lg:items-center'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <IconBadge tone='chart-3'>
            <Share2 />
          </IconBadge>
          <div className='min-w-0'>
            <h3 className='text-sm font-semibold'>{t('Referral Program')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Users who register through your referral link and make purchases earn you commissions. You can transfer commissions to your ordinary wallet at any time.'
              )}
            </p>
          </div>
        </div>
        {loading && <Skeleton className='h-14 rounded-lg' />}
        {!loading && error && (
          <p role='alert' className='text-destructive text-sm'>
            {t('Failed to load')}
          </p>
        )}
        {!loading && !error && (
          <div className='grid grid-cols-2 gap-3 text-center'>
            {[
              [t('Invites'), formatNumber(summary?.invite_count ?? 0, locale)],
              [
                t('Total referral rewards'),
                formatQuotaWithCurrency(summary?.reward_quota ?? 0, {
                  abbreviate: false,
                  roundingMode: 'trunc',
                  locale,
                }),
              ],
            ].map(([label, value]) => (
              <div key={label}>
                <div className='text-muted-foreground text-xs'>{label}</div>
                <div className='mt-1 font-semibold tabular-nums'>{value}</div>
              </div>
            ))}
          </div>
        )}
        <div className='flex items-center gap-2'>
          <Input
            value={affiliateLink}
            readOnly
            aria-label={t('Referral Link')}
            className='min-w-0 font-mono text-xs'
          />
          <CopyButton
            value={affiliateLink}
            variant='outline'
            tooltip={t('Copy referral link')}
            aria-label={t('Copy referral link')}
          />
        </div>
      </CardContent>
    </Card>
  )
}
