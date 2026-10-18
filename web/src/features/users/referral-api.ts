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

export interface ReferralDetail {
  inviter_id: number
  inviter_name: string
  user_id: number
  username: string
  current: boolean
  consumed_quota: number
  reward_quota: number
}

export interface ReferralRecord {
  id: number
  day: number
  settled_at: number
  consumed_quota: number
  rate_basis_points: number
  reward_quota: number
}

export interface CommissionTransfer {
  id: number
  user_id: number
  quota: number
  fee_basis_points: number
  fee_quota: number
  received_quota: number
  created_at: number
}

export interface CommissionSettlementResult {
  day: number
  cutoff: number
  rate_basis_points: number
  settled_charges: number
}

export async function settleTodayCommissions() {
  const response = await api.post<{
    success: boolean
    data: CommissionSettlementResult
  }>('/api/referral/settle_today', {})
  return requireServerSuccess(response.data).data
}

export async function getReferrals(
  page: number,
  pageSize: number,
  keyword: string
) {
  const response = await api.get<{
    success: boolean
    data: { items: ReferralDetail[]; total: number }
  }>('/api/referral/', {
    params: { p: page, page_size: pageSize, keyword },
  })
  return requireServerSuccess(response.data).data
}

export async function getReferralRecords(
  userId: number,
  inviterId: number,
  page: number,
  pageSize: number
) {
  const response = await api.get<{
    success: boolean
    data: { items: ReferralRecord[]; total: number }
  }>(`/api/referral/${userId}/records`, {
    params: { p: page, page_size: pageSize, inviter_id: inviterId },
  })
  return requireServerSuccess(response.data).data
}

export async function changeInviter(userId: number, inviterId: number) {
  const response = await api.put(`/api/user/${userId}/inviter`, {
    inviter_id: inviterId,
  })
  return requireServerSuccess(response.data)
}

export async function getCommissionTransfers(
  page: number,
  pageSize: number,
  userId: string
) {
  const response = await api.get<{
    success: boolean
    data: { items: CommissionTransfer[]; total: number }
  }>('/api/referral/transfers', {
    params: { p: page, page_size: pageSize, user_id: userId || undefined },
  })
  return requireServerSuccess(response.data).data
}
