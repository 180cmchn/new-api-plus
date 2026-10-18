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
import { Reorder } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import { saveWalletOrder, type WalletKind } from '../wallet-api'

export function WalletOrderDialog(props: {
  order: WalletKind[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [order, setOrder] = useState(props.order)
  const labels = {
    ordinary: t('Ordinary wallet'),
    commission: t('Commission'),
    gift: t('Gift credit'),
  }
  const wallets: WalletKind[] = ['ordinary', 'commission', 'gift']
  const mutation = useMutation({
    mutationFn: saveWalletOrder,
    onSuccess: () => {
      toast.success(t('Saved successfully'))
      void client.invalidateQueries({ queryKey: ['wallet-accounts'] })
      props.onClose()
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) props.onClose()
      }}
      title={t('Wallet spending order')}
      description={t(
        'Enabled wallets are charged in order. Removing a wallet disables its spending. Your subscription preference remains unchanged.'
      )}
      footer={
        <Button
          onClick={() => mutation.mutate(order)}
          disabled={mutation.isPending}
        >
          {t('Save')}
        </Button>
      }
    >
      <fieldset disabled={mutation.isPending} className='space-y-4'>
        {order.length === 0 && (
          <p role='status' className='text-muted-foreground text-sm'>
            {t('All wallet spending is disabled')}
          </p>
        )}
        <Reorder.Group
          axis='y'
          values={order}
          onReorder={setOrder}
          className='space-y-2'
        >
          {order.map((wallet, index) => (
            <AutoGroupOrderItem
              key={wallet}
              group={wallet}
              label={labels[wallet]}
              index={index}
              count={order.length}
              onRemove={() =>
                setOrder((current) => current.filter((item) => item !== wallet))
              }
              onMove={(from, direction) =>
                setOrder((current) => {
                  const to = from + (direction === 'up' ? -1 : 1)
                  if (to < 0 || to >= current.length) return current
                  const next = [...current]
                  ;[next[from], next[to]] = [next[to], next[from]]
                  return next
                })
              }
            />
          ))}
        </Reorder.Group>
        <div className='flex flex-wrap gap-2'>
          {wallets
            .filter((wallet) => !order.includes(wallet))
            .map((wallet) => (
              <Button
                key={wallet}
                variant='outline'
                onClick={() => setOrder((current) => [...current, wallet])}
              >
                {t('Enable {{wallet}}', { wallet: labels[wallet] })}
              </Button>
            ))}
        </div>
      </fieldset>
    </Dialog>
  )
}
