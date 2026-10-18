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

import { requireServerSuccess } from '@/lib/server-error-message'

import { getAffiliateCode, getReferralSummary } from '../api'
import { generateAffiliateLink } from '../lib'

export function useAffiliate() {
  const query = useQuery({
    queryKey: ['referral-summary'],
    queryFn: async () => {
      const [codeResponse, summaryResponse] = await Promise.all([
        getAffiliateCode(),
        getReferralSummary(),
      ])
      const code = requireServerSuccess(codeResponse).data
      const summary = requireServerSuccess(summaryResponse).data
      return { affiliateLink: code ? generateAffiliateLink(code) : '', summary }
    },
  })

  return {
    affiliateLink: query.data?.affiliateLink ?? '',
    summary: query.data?.summary,
    loading: query.isPending,
    error: query.isError,
  }
}
