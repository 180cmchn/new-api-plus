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
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { DataTableRow } from '@/components/data-table/core/data-table-row'
import { Button } from '@/components/ui/button'
import { TableCell, TableRow } from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestampToDate, formatNumber } from '@/lib/format'

import {
  getReferrals,
  getReferralRecords,
  type ReferralDetail,
  type ReferralRecord,
} from '../referral-api'

function ReferralRecords({
  userId,
  inviterId,
}: {
  userId: number
  inviterId: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const query = useQuery({
    queryKey: ['referrals', 'records', userId, inviterId, pagination],
    queryFn: () =>
      getReferralRecords(
        userId,
        inviterId,
        pagination.pageIndex + 1,
        pagination.pageSize
      ),
  })
  const columns: ColumnDef<ReferralRecord>[] = [
    {
      accessorKey: 'day',
      header: t('Consumption date (Beijing)'),
      cell: ({ row }) =>
        new Date((row.original.day + 8 * 3600) * 1000)
          .toISOString()
          .slice(0, 10),
    },
    {
      accessorKey: 'settled_at',
      header: t('Settled at'),
      cell: ({ row }) => formatTimestampToDate(row.original.settled_at),
    },
    {
      accessorKey: 'consumed_quota',
      header: t('Ordinary wallet consumption'),
      cell: ({ row }) =>
        formatQuotaWithCurrency(row.original.consumed_quota, {
          abbreviate: false,
          roundingMode: 'trunc',
          locale,
        }),
    },
    {
      accessorKey: 'rate_basis_points',
      header: t('Referral rate'),
      cell: ({ row }) =>
        `${formatNumber(row.original.rate_basis_points / 100, locale)}%`,
    },
    {
      accessorKey: 'reward_quota',
      header: t('Referral reward'),
      cell: ({ row }) =>
        formatQuotaWithCurrency(row.original.reward_quota, {
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
  })
  return (
    <div className='space-y-3 p-3'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Daily commissions use the settlement rate and the inviter at consumption time.'
        )}
      </p>
      {query.isError ? (
        <p role='alert' className='text-destructive'>
          {t('Failed to load')}
        </p>
      ) : (
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={query.isPending}
          hideMobile
          toolbar={null}
          paginationInFooter={false}
          emptyTitle={t('No commission records')}
        />
      )}
    </div>
  )
}

export function ReferralTable() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [keyword, setKeyword] = useState('')
  const [expandedId, setExpandedId] = useState<string>()
  const query = useQuery({
    queryKey: ['referrals', 'users', pagination, keyword],
    queryFn: () =>
      getReferrals(pagination.pageIndex + 1, pagination.pageSize, keyword),
  })
  const columns: ColumnDef<ReferralDetail>[] = [
    {
      accessorKey: 'inviter_name',
      header: t('Inviter'),
      size: 200,
      enableHiding: false,
      cell: ({ row }) => (
        <span>
          {row.original.inviter_name}{' '}
          <span className='text-muted-foreground'>
            #{row.original.inviter_id}
          </span>
        </span>
      ),
    },
    {
      accessorKey: 'username',
      header: t('Invitee'),
      size: 240,
      enableHiding: false,
      cell: ({ row }) => (
        <div>
          <div>
            {row.original.username}{' '}
            <span className='text-muted-foreground'>
              #{row.original.user_id}
            </span>
          </div>
          <div className='text-muted-foreground text-xs'>
            {row.original.current
              ? t('Current binding')
              : t('Historical binding')}
          </div>
        </div>
      ),
    },
    {
      id: 'totals',
      header: t('Consumption / referral reward'),
      size: 360,
      enableHiding: false,
      cell: ({ row }) => (
        <div className='flex items-center justify-between gap-3'>
          <div className='tabular-nums'>
            {formatQuotaWithCurrency(row.original.consumed_quota, {
              abbreviate: false,
              roundingMode: 'trunc',
              locale,
            })}{' '}
            /{' '}
            {formatQuotaWithCurrency(row.original.reward_quota, {
              abbreviate: false,
              roundingMode: 'trunc',
              locale,
            })}
          </div>
          <Button
            variant='ghost'
            size='sm'
            aria-expanded={
              expandedId ===
              `${row.original.inviter_id}-${row.original.user_id}`
            }
            aria-controls={`referral-records-${row.original.inviter_id}-${row.original.user_id}`}
            onClick={() =>
              setExpandedId(
                expandedId ===
                  `${row.original.inviter_id}-${row.original.user_id}`
                  ? undefined
                  : `${row.original.inviter_id}-${row.original.user_id}`
              )
            }
          >
            {expandedId ===
            `${row.original.inviter_id}-${row.original.user_id}` ? (
              <ChevronDown />
            ) : (
              <ChevronRight />
            )}
            {t('Details')}
          </Button>
        </div>
      ),
    },
  ]
  const { table } = useDataTable({
    data: query.data?.items ?? [],
    columns,
    pagination,
    onPaginationChange: setPagination,
    columnFilters: [],
    globalFilter: keyword,
    onGlobalFilterChange: (value) => {
      setKeyword(value)
      setPagination((current) => ({ ...current, pageIndex: 0 }))
      setExpandedId(undefined)
    },
    manualPagination: true,
    manualFiltering: true,
    totalCount: query.data?.total ?? 0,
    enableSorting: false,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <div className='flex min-h-0 flex-1 flex-col gap-3'>
      {query.isError && (
        <p role='alert' className='text-destructive'>
          {t('Failed to load')}
        </p>
      )}
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={query.isPending}
        isFetching={query.isFetching}
        hideMobile
        emptyTitle={t('No invited users')}
        toolbarProps={{
          searchPlaceholder: t('Search inviter or invitee'),
          searchDebounceMs: 400,
        }}
        renderRow={(row) => (
          <Fragment key={row.id}>
            <DataTableRow row={row} cellRenderColumns={columns} />
            {expandedId ===
              `${row.original.inviter_id}-${row.original.user_id}` && (
              <TableRow
                id={`referral-records-${row.original.inviter_id}-${row.original.user_id}`}
              >
                <TableCell colSpan={3} className='bg-muted/20'>
                  <ReferralRecords
                    key={`${row.original.inviter_id}-${row.original.user_id}`}
                    userId={row.original.user_id}
                    inviterId={row.original.inviter_id}
                  />
                </TableCell>
              </TableRow>
            )}
          </Fragment>
        )}
      />
    </div>
  )
}
