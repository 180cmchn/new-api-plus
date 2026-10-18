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
import { useQuery } from '@tanstack/react-query'
import { Activity, BarChart3, Layers } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import type { UserWalletData } from '../types'
import { getWalletTokenSummary } from '../wallet-api'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const tokens = useQuery({
    queryKey: ['wallet-token-summary', props.user?.id],
    queryFn: getWalletTokenSummary,
    enabled: Boolean(props.user?.id),
  })
  const tokensLoading = Boolean(props.user?.id) && tokens.isPending

  const stats: {
    label: string
    value: string
    description: string
    icon: typeof Activity
    tone: IconBadgeTone
    loading?: boolean
    error?: boolean
  }[] = [
    {
      label: t('Total Usage'),
      value: formatQuotaWithCurrency(props.user?.used_quota ?? 0, {
        roundingMode: 'trunc',
        locale,
      }),
      description: t('Total consumed quota'),
      icon: BarChart3,
      tone: 'info',
    },
    {
      label: t('API Requests'),
      value: formatNumber(props.user?.request_count ?? 0, locale),
      description: t('Total requests made'),
      icon: Activity,
      tone: 'chart-4',
    },
    {
      label: t('Cumulative tokens'),
      value: formatNumber(tokens.data?.total_tokens, locale),
      description: t('Total recorded input and output tokens'),
      icon: Layers,
      tone: 'chart-3',
      loading: tokensLoading,
      error: tokens.isError,
    },
  ]

  return (
    <section
      aria-label={t('Wallet statistics')}
      aria-busy={Boolean(props.loading || tokensLoading)}
      className='grid grid-cols-1 gap-4 rounded-lg border p-4 sm:grid-cols-3'
    >
      {stats.map((item) => (
        <div
          key={item.label}
          role='group'
          aria-label={item.label}
          className='min-w-0'
        >
          <div className='flex items-center gap-1.5 sm:gap-2.5'>
            <IconBadge tone={item.tone} size='stat'>
              <item.icon />
            </IconBadge>
            <h2 className='text-muted-foreground truncate text-[11px] font-medium tracking-wider uppercase sm:text-xs'>
              {item.label}
            </h2>
          </div>

          <div className='text-foreground mt-1.5 font-mono text-sm font-bold tracking-tight break-all tabular-nums sm:mt-2.5 sm:text-2xl'>
            {props.loading || item.loading ? (
              <Skeleton className='h-5 w-28 sm:h-8' />
            ) : (
              item.value
            )}
          </div>
          {item.error ? (
            <div className='text-muted-foreground mt-1 flex items-center gap-2 text-xs'>
              <span>{t('Failed to load')}</span>
              <Button
                variant='link'
                size='sm'
                className='h-auto p-0 text-xs'
                disabled={tokens.isFetching}
                onClick={() => void tokens.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          ) : (
            <div className='text-muted-foreground/60 mt-1 hidden text-xs md:block'>
              {item.description}
            </div>
          )}
        </div>
      ))}
    </section>
  )
}
