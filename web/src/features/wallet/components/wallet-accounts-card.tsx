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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getWalletBalances, transferCommission } from '../wallet-api'
import { TransferDialog } from './dialogs/transfer-dialog'
import { WalletOrderDialog } from './wallet-order-dialog'

export function WalletAccountsCard(props: { onBalanceChange: () => void }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const userID = useAuthStore((state) => state.auth.user?.id)
  const [transferOpen, setTransferOpen] = useState(false)
  const [orderOpen, setOrderOpen] = useState(false)
  const request = useRef<{ quota: number; fee: number; id: string } | null>(
    null
  )
  const query = useQuery({
    queryKey: ['wallet-accounts', userID],
    queryFn: getWalletBalances,
  })
  const mutation = useMutation({
    mutationFn: async (quota: number) => {
      if (!query.data) throw new Error(t('Failed to load'))
      const fee = query.data.transfer_fee_basis_points
      if (request.current?.quota !== quota || request.current.fee !== fee) {
        request.current = { quota, fee, id: crypto.randomUUID() }
      }
      return transferCommission(quota, fee, request.current.id)
    },
    onSuccess: () => {
      toast.success(t('Transfer successful'))
      void client.invalidateQueries({ queryKey: ['wallet-accounts'] })
      props.onBalanceChange()
    },
    onError: (error) => {
      handleServerError(error)
      void client.invalidateQueries({ queryKey: ['wallet-accounts'] })
    },
  })
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data) return <Skeleton className='h-40 w-full rounded-lg' />
  const wallets = query.data
  const balances = [
    { label: t('Ordinary wallet'), value: wallets.ordinary_quota },
    { label: t('Commission'), value: wallets.commission_quota },
    { label: t('Gift credit'), value: wallets.gift_quota },
  ]
  return (
    <section
      className='rounded-lg border p-4'
      aria-label={t('Wallet balances')}
    >
      <div className='grid gap-4 sm:grid-cols-3'>
        {balances.map((wallet) => (
          <div key={wallet.label} className='min-w-0 space-y-2'>
            <h2 className='text-muted-foreground text-sm'>{wallet.label}</h2>
            <p className='text-xl font-semibold break-all tabular-nums'>
              {formatQuotaWithCurrency(wallet.value, {
                abbreviate: false,
                roundingMode: 'trunc',
                locale,
              })}
            </p>
          </div>
        ))}
      </div>
      <div className='mt-4 flex flex-wrap gap-2'>
        <Button variant='outline' onClick={() => setOrderOpen(true)}>
          {t('Wallet spending order')}
        </Button>
        <Button
          disabled={wallets.commission_quota <= 0}
          onClick={() => {
            request.current = null
            setTransferOpen(true)
          }}
        >
          {t('Transfer commission to ordinary wallet')}
        </Button>
      </div>
      {orderOpen && (
        <WalletOrderDialog
          order={wallets.wallet_order}
          onClose={() => setOrderOpen(false)}
        />
      )}
      {transferOpen && (
        <TransferDialog
          open
          onOpenChange={setTransferOpen}
          availableQuota={wallets.commission_quota}
          feeBasisPoints={wallets.transfer_fee_basis_points}
          transferring={mutation.isPending}
          onConfirm={async (quota) => {
            try {
              await mutation.mutateAsync(quota)
              return true
            } catch {
              return false
            }
          }}
        />
      )}
    </section>
  )
}
