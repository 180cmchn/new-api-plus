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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import {
  formatNumber,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'

interface TransferDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: (amount: number) => Promise<boolean>
  availableQuota: number
  transferring: boolean
  feeBasisPoints?: number
}

export function TransferDialog(props: TransferDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [amount, setAmount] = useState('')
  const [all, setAll] = useState(false)
  // Keep the raw quota when All is selected: converting a formatted display
  // amount back to quota can leave a residual balance or exceed the balance.
  const quota = all
    ? props.availableQuota
    : parseQuotaFromDollars(Number(amount), 'trunc')
  const feeRate = props.feeBasisPoints ?? 0
  const validQuota =
    Number.isSafeInteger(quota) && quota > 0 && quota <= props.availableQuota
  const received = validQuota
    ? Number((BigInt(quota) * BigInt(10000 - feeRate)) / 10000n)
    : 0
  const fee = validQuota ? quota - received : 0

  return (
    <ConfirmDialog
      open={props.open}
      onOpenChange={(open) => {
        if (!props.transferring) props.onOpenChange(open)
      }}
      title={t('Transfer commission to ordinary wallet')}
      desc={t(
        'The fee is deducted from the transfer amount. Commission and gift credit cannot be refunded directly.'
      )}
      confirmText={t('Confirm transfer')}
      isLoading={props.transferring}
      disabled={!validQuota || received <= 0}
      handleConfirm={async () => {
        if (validQuota && received > 0 && (await props.onConfirm(quota))) {
          props.onOpenChange(false)
        }
      }}
    >
      <div className='space-y-4'>
        <p>
          {t('Commission')}:{' '}
          {formatQuotaWithCurrency(props.availableQuota, {
            abbreviate: false,
            roundingMode: 'trunc',
            locale,
          })}
        </p>
        <div className='space-y-2'>
          <Label htmlFor='commission-transfer-amount'>
            {t('Transfer Amount')}
          </Label>
          <InputGroup>
            <InputGroupInput
              id='commission-transfer-amount'
              type='number'
              min={0}
              max={quotaUnitsToDollars(props.availableQuota)}
              step='any'
              value={
                all ? String(quotaUnitsToDollars(props.availableQuota)) : amount
              }
              disabled={props.transferring}
              onChange={(event) => {
                setAll(false)
                setAmount(event.target.value)
              }}
            />
            <InputGroupAddon align='inline-end'>
              <InputGroupButton
                aria-pressed={all}
                disabled={props.transferring || props.availableQuota <= 0}
                onClick={() => setAll(true)}
              >
                {t('All')}
              </InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </div>
        <dl className='grid grid-cols-2 gap-2 text-sm tabular-nums'>
          <dt>
            {t('Transfer fee')} ({formatNumber(feeRate / 100, locale)}%)
          </dt>
          <dd className='text-right'>
            {formatQuotaWithCurrency(fee, {
              abbreviate: false,
              digitsSmall: 6,
              roundingMode: 'trunc',
              locale,
            })}
          </dd>
          <dt>{t('Amount received')}</dt>
          <dd className='text-right font-semibold'>
            {formatQuotaWithCurrency(received, {
              abbreviate: false,
              digitsSmall: 6,
              roundingMode: 'trunc',
              locale,
            })}
          </dd>
        </dl>
      </div>
    </ConfirmDialog>
  )
}
