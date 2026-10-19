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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { AffiliateRewardsCard } from '@/features/wallet/components/affiliate-rewards-card'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { CommissionTransfersTable } from '../commission-transfers-table'
import { InviterDialog } from '../inviter-dialog'
import { ReferralTable } from '../referral-table'
import { SettleTodayCommissionsButton } from '../settle-today-commissions-button'

const clients: QueryClient[] = []
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

test.each([10, 100])(
  'role %i confirms today settlement, blocks repeat clicks and refreshes balances',
  async (role) => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role })
    let complete!: () => void
    const completed = new Promise<void>((resolve) => {
      complete = resolve
    })
    const post = vi.spyOn(api, 'post').mockImplementationOnce(async () => {
      await completed
      return {
        data: {
          success: true,
          data: {
            day: 100,
            cutoff: 200,
            rate_basis_points: 1250,
            settled_charges: 2,
          },
        },
      }
    })
    const success = vi.spyOn(toast, 'success')
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    clients.push(client)
    const onSettled = vi.fn()
    render(
      <QueryClientProvider client={client}>
        <SettleTodayCommissionsButton onSettled={onSettled} />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    const button = screen.getByRole('button', {
      name: "Settle today's rewards (test)",
    })
    await user.click(button)
    expect(post).not.toHaveBeenCalled()
    expect(screen.getByRole('alertdialog')).toHaveTextContent(
      '00:00 Beijing time'
    )
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(post).not.toHaveBeenCalled()
    await user.click(button)
    await user.click(screen.getByRole('button', { name: 'Settle now' }))
    expect(
      await screen.findByRole('button', { name: 'Settling...' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/api/referral/settle_today', {})
    complete()
    await waitFor(() => expect(onSettled).toHaveBeenCalledOnce())
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(success).toHaveBeenCalledWith(
      'Settled 2 consumption records at 12.5%.'
    )
    expect(button).toBeEnabled()
  }
)

test('a settlement error keeps the dialog open and an empty retry reports no eligible consumption', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: {
        success: false,
        message:
          'Enable referral rewards and set a positive rate before settling',
      },
    })
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          day: 100,
          cutoff: 200,
          rate_basis_points: 1000,
          settled_charges: 0,
        },
      },
    })
  const error = vi.spyOn(toast, 'error')
  const success = vi.spyOn(toast, 'success')
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  clients.push(client)
  const onSettled = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <SettleTodayCommissionsButton onSettled={onSettled} />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(
    screen.getByRole('button', { name: "Settle today's rewards (test)" })
  )
  await user.click(screen.getByRole('button', { name: 'Settle now' }))
  await waitFor(() => expect(error).toHaveBeenCalled())
  expect(screen.getByRole('alertdialog')).toBeVisible()
  expect(onSettled).not.toHaveBeenCalled()
  expect(success).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Settle now' }))
  await waitFor(() => expect(onSettled).toHaveBeenCalledOnce())
  expect(post).toHaveBeenCalledTimes(2)
  expect(success).toHaveBeenCalledWith(
    'No unsettled eligible consumption today'
  )
})

test('an ordinary user has no manual settlement button', () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'user', role: 1 })
  const client = new QueryClient()
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <SettleTodayCommissionsButton onSettled={vi.fn()} />
    </QueryClientProvider>
  )
  expect(
    screen.queryByRole('button', { name: "Settle today's rewards (test)" })
  ).not.toBeInTheDocument()
})

test('changing an inviter rejects self binding and requires a valid user ID', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  clients.push(client)
  const onClose = vi.fn()
  const onSuccess = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <InviterDialog
        user={{
          id: 2,
          username: 'B',
          display_name: 'B',
          role: 1,
          status: 1,
          quota: 0,
          used_quota: 0,
          request_count: 0,
          group: 'default',
          inviter_id: 1,
        }}
        onClose={onClose}
        onSuccess={onSuccess}
      />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  const dialog = screen.getByRole('alertdialog')
  const input = within(dialog).getByRole('spinbutton', {
    name: 'Inviter user ID',
  })
  expect(input).toHaveValue(1)
  expect(
    within(dialog).getByText(
      /Past consumption and rewards keep their original owner/
    )
  ).toBeVisible()
  await user.clear(input)
  await user.type(input, '2')
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('Invalid inviter')).toBeVisible()
  expect(put).not.toHaveBeenCalled()
  await user.clear(input)
  await user.type(input, '3')
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce())
  expect(onClose).toHaveBeenCalledOnce()
  expect(put).toHaveBeenCalledWith('/api/user/2/inviter', { inviter_id: 3 })
})

test('the user sees only invite count, effective rewards and the referral link', () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 500000 },
  })
  render(
    <AffiliateRewardsCard
      summary={{ invite_count: 3, reward_quota: 4931 }}
      affiliateLink='https://example.com/register?aff=A'
    />
  )
  expect(screen.getByText('Invites')).toBeVisible()
  expect(screen.getByText('3')).toBeVisible()
  expect(screen.getByText('Total referral rewards')).toBeVisible()
  expect(screen.getByText('$0.0098')).toBeVisible()
  expect(screen.getByRole('textbox', { name: 'Referral Link' })).toHaveValue(
    'https://example.com/register?aff=A'
  )
  expect(screen.queryByText('Pending')).not.toBeInTheDocument()
  expect(screen.queryByText('Transfer to Balance')).not.toBeInTheDocument()
  expect(screen.queryByText('Invitee')).not.toBeInTheDocument()
  const withdrawalCopy = screen.getByText('withdrawals are supported')
  expect(withdrawalCopy).toBeVisible()
  expect(withdrawalCopy.closest('p')).toHaveTextContent(
    'Users who register through your referral link and make purchases earn you commissions. You can transfer commissions to your ordinary wallet at any time, and withdrawals are supported.'
  )
})

test('the admin sees transfer amounts and fees and filters by user ID', async () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 500000 },
  })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 1,
        items: [
          {
            id: 1,
            user_id: 2,
            quota: 4931,
            fee_basis_points: 1000,
            fee_quota: 494,
            received_quota: 4437,
            created_at: 1721001600,
          },
        ],
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const router = createRouter({
    routeTree: createRootRoute({ component: CommissionTransfersTable }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('$0.0098')).toBeVisible()
  expect(screen.getByText('$0.0009')).toBeVisible()
  expect(screen.getByText('$0.0088')).toBeVisible()
  const user = userEvent.setup()
  await user.type(screen.getByPlaceholderText('Search user ID'), '2')
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/referral/transfers', {
      params: { p: 1, page_size: 20, user_id: '2' },
    })
  )
})

test('the admin expands daily commissions with preserved rates and consumption amounts', async () => {
  const user = userEvent.setup()
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 500000 },
  })
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/referral/') {
      return {
        data: {
          success: true,
          data: {
            total: 3,
            items: [
              {
                inviter_id: 1,
                inviter_name: 'A',
                user_id: 2,
                username: 'B',
                current: true,
                consumed_quota: 49310,
                reward_quota: 4931,
              },
              {
                inviter_id: 1,
                inviter_name: 'A',
                user_id: 3,
                username: 'C',
                current: true,
                consumed_quota: 10000000,
                reward_quota: 1000000,
              },
              {
                inviter_id: 1,
                inviter_name: 'A',
                user_id: 4,
                username: 'D',
                current: true,
                consumed_quota: 0,
                reward_quota: 0,
              },
            ],
          },
        },
      }
    }
    if (url === '/api/referral/2/records') {
      return {
        data: {
          success: true,
          data: {
            total: 2,
            items: [
              {
                id: 1,
                day: 1721001600,
                consumed_quota: 49310,
                rate_basis_points: 1000,
                reward_quota: 4931,
                settled_at: 1000,
              },
              {
                id: 2,
                day: 1721088000,
                consumed_quota: 2000000,
                rate_basis_points: 1250,
                reward_quota: 250000,
                settled_at: 1500,
              },
            ],
          },
        },
      }
    }
    throw new Error(`Unexpected URL: ${url}`)
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const router = createRouter({
    routeTree: createRootRoute({ component: ReferralTable }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  const name = await screen.findByText('B')
  const row = name.closest('tr')
  const compensationRow = screen.getByText('D').closest('tr')
  if (!row || !compensationRow) {
    throw new Error('Expected both invited user rows')
  }
  expect(within(row).getByText('$0.0986 / $0.0098')).toBeVisible()
  expect(within(compensationRow).getByText('$0 / $0')).toBeVisible()
  await user.click(within(row).getByRole('button', { name: 'Details' }))
  expect(await screen.findByText('12.5%')).toBeVisible()
  expect(screen.getByText('10%')).toBeVisible()
  expect(screen.getByText('$0.0098')).toBeVisible()
  expect(screen.queryByText('$0.0099')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Revoke redemption' })
  ).not.toBeInTheDocument()
  expect(get).toHaveBeenCalledWith(
    '/api/referral/2/records',
    expect.objectContaining({
      params: expect.objectContaining({ p: 1, page_size: 10, inviter_id: 1 }),
    })
  )
})
