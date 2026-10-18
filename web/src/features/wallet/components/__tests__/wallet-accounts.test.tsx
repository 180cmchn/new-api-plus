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

import { WalletAccountsCard } from '../wallet-accounts-card'

const clients: QueryClient[] = []
const balances = {
  ordinary_quota: -5000000,
  commission_quota: 50000000,
  gift_quota: 2500000,
  wallet_order: ['ordinary', 'commission', 'gift'],
  transfer_fee_basis_points: 1000,
}

beforeEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 500000 },
  })
})
afterEach(async () => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  await act(() => i18next.changeLanguage('en'))
})

function renderWallet(onBalanceChange = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <WalletAccountsCard onBalanceChange={onBalanceChange} />
    </QueryClientProvider>
  )
  return onBalanceChange
}

test('three separate balances include debt and a transfer shows its fee before confirmation', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: balances },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockRejectedValueOnce(new Error('Temporarily unavailable'))
    .mockResolvedValue({ data: { success: true, data: {} } })
  const changed = renderWallet()
  const user = userEvent.setup()
  const wallets = await screen.findByRole('region', { name: 'Wallet balances' })
  expect(within(wallets).getByText('-$10')).toBeVisible()
  expect(within(wallets).getByText('$100')).toBeVisible()
  expect(within(wallets).getByText('$5')).toBeVisible()
  await user.click(
    screen.getByRole('button', {
      name: 'Transfer commission to ordinary wallet',
    })
  )
  const dialog = screen.getByRole('alertdialog')
  const confirm = within(dialog).getByRole('button', {
    name: 'Confirm transfer',
  })
  expect(confirm).toBeDisabled()
  await user.type(
    within(dialog).getByRole('spinbutton', { name: 'Transfer Amount' }),
    '100'
  )
  expect(within(dialog).getByText('$10')).toBeVisible()
  expect(within(dialog).getByText('$90')).toBeVisible()
  expect(
    within(dialog).getByText(/fee is deducted from the transfer amount/)
  ).toBeVisible()
  await user.click(confirm)
  await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(confirm).toBeEnabled())
  expect(dialog).toBeVisible()
  await user.click(confirm)
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(post).toHaveBeenCalledTimes(2)
  expect(post.mock.calls[0]).toEqual(post.mock.calls[1])
  expect(post.mock.calls[0]).toEqual([
    '/api/user/wallet/transfer',
    {
      quota: 50000000,
      fee_basis_points: 1000,
      request_id: expect.any(String),
    },
  ])
  expect(changed).toHaveBeenCalledOnce()
})

test('wallet order supports keyboard reordering and disabling wallets while saving stable wallet IDs', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: balances },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderWallet()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Wallet spending order' })
  )
  const dialog = screen.getByRole('dialog')
  const drag = within(dialog).getByRole('button', {
    name: 'Drag Gift credit to reorder',
  })
  drag.focus()
  await user.keyboard('{ArrowUp}')
  await user.click(
    within(dialog).getByRole('button', { name: 'Remove Commission' })
  )
  expect(
    within(dialog).getByRole('button', { name: 'Enable Commission' })
  ).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/user/wallet/order', {
      wallet_order: ['ordinary', 'gift'],
    })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

test('all wallets can be disabled and zero commission prevents transferring', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { ...balances, commission_quota: 0, wallet_order: [] },
    },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderWallet()
  const user = userEvent.setup()
  expect(
    await screen.findByRole('button', {
      name: 'Transfer commission to ordinary wallet',
    })
  ).toBeDisabled()
  await user.click(
    screen.getByRole('button', { name: 'Wallet spending order' })
  )
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByRole('status')).toHaveTextContent(
    'All wallet spending is disabled'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/user/wallet/order', {
      wallet_order: [],
    })
  )
})

test('a failed wallet fetch can be retried without showing invented zero balances', async () => {
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue({ data: { success: true, data: balances } })
  renderWallet()
  const user = userEvent.setup()
  expect(await screen.findByText('Failed to load')).toBeVisible()
  expect(
    screen.queryByRole('region', { name: 'Wallet balances' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByRole('region', { name: 'Wallet balances' })
  ).toBeVisible()
})

test('All transfers the full small commission balance and prevents changes while submitting', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...balances, commission_quota: 4931 } },
  })
  let complete!: () => void
  const completed = new Promise<void>((resolve) => {
    complete = resolve
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async () => {
    await completed
    return { data: { success: true } }
  })
  renderWallet()
  const user = userEvent.setup()
  const wallets = await screen.findByRole('region', { name: 'Wallet balances' })
  expect(within(wallets).getByText('$0.0098')).toBeVisible()
  await user.click(
    screen.getByRole('button', {
      name: 'Transfer commission to ordinary wallet',
    })
  )
  const dialog = screen.getByRole('alertdialog')
  const all = within(dialog).getByRole('button', { name: 'All' })
  all.focus()
  await user.keyboard('{Enter}')
  const input = within(dialog).getByRole('spinbutton', {
    name: 'Transfer Amount',
  })
  expect(input).toHaveValue(0.009862)
  expect(all).toHaveAttribute('aria-pressed', 'true')
  expect(within(dialog).getByText('$0.000988')).toBeVisible()
  expect(within(dialog).getByText('$0.008874')).toBeVisible()
  expect(post).not.toHaveBeenCalled()
  const confirm = within(dialog).getByRole('button', {
    name: 'Confirm transfer',
  })
  await user.click(confirm)
  await waitFor(() => expect(post).toHaveBeenCalledOnce())
  expect(post).toHaveBeenCalledWith('/api/user/wallet/transfer', {
    quota: 4931,
    fee_basis_points: 1000,
    request_id: expect.any(String),
  })
  expect(all).toBeDisabled()
  expect(input).toBeDisabled()
  expect(confirm).toBeDisabled()
  complete()
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
})

test('editing after All switches to the entered amount and truncates fractional quota', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...balances, commission_quota: 4931 } },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderWallet()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', {
      name: 'Transfer commission to ordinary wallet',
    })
  )
  const dialog = screen.getByRole('alertdialog')
  const all = within(dialog).getByRole('button', { name: 'All' })
  await user.click(all)
  const input = within(dialog).getByRole('spinbutton', {
    name: 'Transfer Amount',
  })
  const confirm = within(dialog).getByRole('button', {
    name: 'Confirm transfer',
  })
  await user.clear(input)
  await user.type(input, '0.0000019')
  expect(all).toHaveAttribute('aria-pressed', 'false')
  expect(confirm).toBeDisabled()
  await user.clear(input)
  await user.type(input, '0.0009059')
  await user.click(confirm)
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/wallet/transfer', {
      quota: 452,
      fee_basis_points: 1000,
      request_id: expect.any(String),
    })
  )
})

test('wallet amounts react to a language change while retaining truncation', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...balances, commission_quota: 4931 } },
  })
  renderWallet()
  expect(await screen.findByText('$0.0098')).toBeVisible()
  i18next.addResourceBundle('fr', 'translation', {})
  await act(() => i18next.changeLanguage('fr'))
  expect(screen.getByText('0,0098 $')).toBeVisible()
  expect(screen.queryByText('$0.0098')).not.toBeInTheDocument()
})
