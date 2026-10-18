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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { WalletStatsCard } from '../wallet-stats-card'

const clients: QueryClient[] = []
const user = {
  id: 1,
  username: 'alice',
  quota: 5000000,
  used_quota: 106250,
  request_count: 5,
  aff_quota: 0,
  aff_history_quota: 0,
  aff_count: 0,
  group: 'default',
}

beforeEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})
afterEach(async () => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  await act(() => i18next.changeLanguage('en'))
})

function renderStats(loading = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={loading ? null : user} loading={loading} />
    </QueryClientProvider>
  )
}

test('historical tokens appear after API requests in a three-column wallet summary', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { total_tokens: 4294967300 } },
  })
  renderStats()
  const stats = screen.getByRole('region', { name: 'Wallet statistics' })
  expect(stats).toHaveClass('grid-cols-1', 'sm:grid-cols-3', 'gap-4', 'p-4')
  expect(
    within(stats)
      .getAllByRole('group')
      .map((item) => item.getAttribute('aria-label'))
  ).toEqual(['Total Usage', 'API Requests', 'Cumulative tokens'])
  expect(await within(stats).findByText('4,294,967,300')).toBeVisible()
  expect(within(stats).getByText('$0.2125')).toBeVisible()
  expect(within(stats).getByText('5')).toBeVisible()
  expect(
    within(stats).getByText('Total recorded input and output tokens')
  ).toBeInTheDocument()
  expect(get).toHaveBeenCalledWith('/api/data/self/summary')
})

test('the initial loading state retains three columns without querying an unknown user', () => {
  const get = vi.spyOn(api, 'get')
  renderStats(true)
  const stats = screen.getByRole('region', { name: 'Wallet statistics' })
  expect(stats).toHaveClass('sm:grid-cols-3')
  expect(stats).toHaveAttribute('aria-busy', 'true')
  expect(within(stats).getAllByRole('group')).toHaveLength(3)
  expect(get).not.toHaveBeenCalled()
})

test('a failed token request shows a retry instead of a false zero total', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: false, message: 'unavailable' } })
    .mockResolvedValueOnce({
      data: { success: true, data: { total_tokens: 0 } },
    })
  renderStats()
  const tokens = screen.getByRole('group', { name: 'Cumulative tokens' })
  const retry = await within(tokens).findByRole('button', { name: 'Retry' })
  expect(within(tokens).getByText('Failed to load')).toBeVisible()
  expect(within(tokens).queryByText('0')).not.toBeInTheDocument()
  await userEvent.setup().click(retry)
  expect(await within(tokens).findByText('0')).toBeVisible()
  expect(
    within(tokens).queryByRole('button', { name: 'Retry' })
  ).not.toBeInTheDocument()
})

test('switching users requests a separate total and does not reuse the previous user statistics', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: { success: true, data: { total_tokens: 123 } },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: { total_tokens: 456 } },
    })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const view = render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={user} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('123')).toBeVisible()
  view.rerender(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={{ ...user, id: 2, username: 'bob' }} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('456')).toBeVisible()
  expect(screen.queryByText('123')).not.toBeInTheDocument()
  expect(get).toHaveBeenCalledTimes(2)
})

test('token formatting updates when the interface language changes', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { total_tokens: 1234567 } },
  })
  renderStats()
  expect(await screen.findByText('1,234,567')).toBeVisible()
  i18next.addResourceBundle('fr', 'translation', {})
  await act(() => i18next.changeLanguage('fr'))
  await waitFor(() => expect(screen.getByText('1 234 567')).toBeVisible())
  expect(screen.queryByText('1,234,567')).not.toBeInTheDocument()
})
