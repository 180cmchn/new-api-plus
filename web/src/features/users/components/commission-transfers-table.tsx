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
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber, formatTimestampToDate } from '@/lib/format'

import {
  getCommissionTransfers,
  type CommissionTransfer,
} from '../referral-api'

export function CommissionTransfersTable() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [userID, setUserID] = useState('')
  const query = useQuery({
    queryKey: ['commission-transfers', pagination, userID],
    queryFn: () =>
      getCommissionTransfers(
        pagination.pageIndex + 1,
        pagination.pageSize,
        userID
      ),
  })
  const columns: ColumnDef<CommissionTransfer>[] = [
    { accessorKey: 'user_id', header: t('User ID') },
    {
      accessorKey: 'created_at',
      header: t('Time'),
      cell: ({ row }) => formatTimestampToDate(row.original.created_at),
    },
    {
      accessorKey: 'quota',
      header: t('Transfer Amount'),
      cell: ({ row }) =>
        formatQuotaWithCurrency(row.original.quota, {
          abbreviate: false,
          roundingMode: 'trunc',
          locale,
        }),
    },
    {
      accessorKey: 'fee_basis_points',
      header: t('Commission transfer fee (%)'),
      cell: ({ row }) =>
        formatNumber(row.original.fee_basis_points / 100, locale),
    },
    {
      accessorKey: 'fee_quota',
      header: t('Transfer fee'),
      cell: ({ row }) =>
        formatQuotaWithCurrency(row.original.fee_quota, {
          abbreviate: false,
          roundingMode: 'trunc',
          locale,
        }),
    },
    {
      accessorKey: 'received_quota',
      header: t('Amount received'),
      cell: ({ row }) =>
        formatQuotaWithCurrency(row.original.received_quota, {
          abbreviate: false,
          roundingMode: 'trunc',
          locale,
        }),
    },
  ]
  const { table } = useDataTable({
    data: query.data?.items ?? [],
    columns,
    pagination,
    onPaginationChange: setPagination,
    manualPagination: true,
    totalCount: query.data?.total ?? 0,
    enableSorting: false,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
    columnFilters: [],
    globalFilter: userID,
    onGlobalFilterChange: (value) => {
      setUserID(value)
      setPagination((current) => ({ ...current, pageIndex: 0 }))
    },
    manualFiltering: true,
  })
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <DataTablePage
      table={table}
      columns={columns}
      hideMobile
      isLoading={query.isPending}
      emptyTitle={t('No commission records')}
      toolbarProps={{
        searchPlaceholder: t('Search user ID'),
        searchDebounceMs: 400,
      }}
    />
  )
}
