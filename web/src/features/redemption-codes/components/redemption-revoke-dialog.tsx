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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { revokeRedemption } from '../api'

export function RedemptionRevokeDialog({
  redemptionId,
  open,
  onOpenChange,
  onSuccess,
}: {
  redemptionId?: number
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await revokeRedemption(id)),
    onSuccess: () => {
      toast.success(t('Redemption revoked'))
      onOpenChange(false)
      onSuccess?.()
      void client.invalidateQueries({ queryKey: ['referrals'] })
      void client.invalidateQueries({ queryKey: ['referral-summary'] })
      void client.invalidateQueries({ queryKey: ['redemptions'] })
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Revoke redemption')}
      desc={t(
        'Reverse code #{{id}} from its original wallet. Balances may become negative. Historical recharge rewards are reversed using their original records; consumption commissions remain unchanged. The code cannot be reused.',
        { id: redemptionId }
      )}
      confirmText={t('Revoke redemption')}
      destructive
      isLoading={mutation.isPending}
      disabled={!redemptionId}
      handleConfirm={() => {
        if (redemptionId) mutation.mutate(redemptionId)
      }}
    />
  )
}
