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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type WalletKind = 'ordinary' | 'commission' | 'gift'
export interface WalletBalances {
  ordinary_quota: number
  commission_quota: number
  gift_quota: number
  wallet_order: WalletKind[]
  transfer_fee_basis_points: number
}

export async function getWalletBalances() {
  const response = await api.get<{ success: boolean; data: WalletBalances }>(
    '/api/user/wallet'
  )
  return requireServerSuccess(response.data).data
}

export async function getWalletTokenSummary() {
  const response = await api.get<{
    success: boolean
    data: { total_tokens: number }
  }>('/api/data/self/summary')
  return requireServerSuccess(response.data).data
}

export async function saveWalletOrder(order: WalletKind[]) {
  const response = await api.put('/api/user/wallet/order', {
    wallet_order: order,
  })
  return requireServerSuccess(response.data)
}

export async function transferCommission(
  quota: number,
  fee: number,
  requestID: string
) {
  const response = await api.post('/api/user/wallet/transfer', {
    quota,
    fee_basis_points: fee,
    request_id: requestID,
  })
  return requireServerSuccess(response.data)
}
