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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { settleTodayCommissions } from '../referral-api'

export function SettleTodayCommissionsButton(props: { onSettled: () => void }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const isAdmin = useAuthStore((state) => (state.auth.user?.role ?? 0) >= 10)
  const [open, setOpen] = useState(false)
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: settleTodayCommissions,
    retry: false,
    onSuccess: (result) => {
      setOpen(false)
      if (result.settled_charges === 0) {
        toast.success(t('No unsettled eligible consumption today'))
      } else {
        toast.success(
          t('Settled {{count}} consumption records at {{rate}}%.', {
            count: formatNumber(result.settled_charges, locale),
            rate: formatNumber(result.rate_basis_points / 100, locale),
          })
        )
      }
      void client.invalidateQueries({ queryKey: ['referrals'] })
      void client.invalidateQueries({ queryKey: ['wallet-accounts'] })
      void client.invalidateQueries({ queryKey: ['referral-summary'] })
      props.onSettled()
    },
    onError: (error) => handleServerError(error),
  })

  if (!isAdmin) return null

  return (
    <>
      <Button
        size='sm'
        variant='outline'
        disabled={mutation.isPending}
        aria-busy={mutation.isPending}
        onClick={() => setOpen(true)}
      >
        {t("Settle today's rewards (test)")}
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={(value) => {
          if (!mutation.isPending) setOpen(value)
        }}
        title={t("Settle today's referral rewards?")}
        desc={t(
          'Settle unpaid ordinary wallet consumption from 00:00 Beijing time until now using the current referral rate. Rewards will be credited as commission. Paid consumption will not be settled again.'
        )}
        confirmText={mutation.isPending ? t('Settling...') : t('Settle now')}
        isLoading={mutation.isPending}
        handleConfirm={() => mutation.mutate()}
      />
    </>
  )
}
