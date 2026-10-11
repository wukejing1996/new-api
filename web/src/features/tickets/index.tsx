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
import type { ColumnDef, PaginationState } from '@tanstack/react-table'
import { RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { TimestampCell } from '@/components/activity-time-cell'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getTicketCounts, getTickets } from './api'
import { TicketDialog } from './components/ticket-dialog'
import {
  ticketCategoryLabels,
  ticketPriorityLabels,
  ticketStatusLabels,
  type Ticket,
  type TicketFilter,
} from './types'

export function Tickets() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })
  const [keyword, setKeyword] = useState('')
  const [filter, setFilter] = useState<TicketFilter>('all')
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const list = useQuery({
    queryKey: ['admin-tickets', 'list', pagination, keyword, filter],
    queryFn: ({ signal }) =>
      getTickets(
        {
          page: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
          keyword,
          filter,
        },
        signal
      ),
    placeholderData: (previous) => previous,
  })
  const counts = useQuery({
    queryKey: ['admin-tickets', 'counts'],
    queryFn: ({ signal }) => getTicketCounts(signal),
  })
  const columns = useMemo<ColumnDef<Ticket>[]>(
    () => [
      { accessorKey: 'id', header: t('ID'), size: 70 },
      {
        accessorKey: 'subject',
        header: t('Subject'),
        size: 320,
        cell: ({ row }) => (
          <div className='flex min-w-0 items-center gap-2'>
            {row.original.unread && (
              <span
                className='size-2 shrink-0 rounded-full bg-amber-500'
                aria-label={t('Unread')}
              />
            )}
            <Button
              variant='link'
              className='h-auto min-w-0 justify-start p-0 text-left break-words whitespace-normal'
              onClick={() => setSelectedId(row.original.id)}
            >
              {row.original.subject}
            </Button>
          </div>
        ),
      },
      {
        accessorKey: 'user_name',
        header: t('Submitted by'),
        cell: ({ row }) => (
          <span className='break-all'>
            {row.original.user_name || `#${row.original.user_id}`}
          </span>
        ),
      },
      {
        accessorKey: 'category',
        header: t('Category'),
        cell: ({ row }) =>
          t(
            ticketCategoryLabels[row.original.category] || row.original.category
          ),
      },
      {
        accessorKey: 'priority',
        header: t('Priority'),
        cell: ({ row }) =>
          t(
            ticketPriorityLabels[row.original.priority] || row.original.priority
          ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => (
          <div className='flex flex-wrap gap-1'>
            <Badge variant='outline'>
              {t(ticketStatusLabels[row.original.status])}
            </Badge>
            {row.original.unreplied && (
              <Badge variant='secondary'>{t('Awaiting reply')}</Badge>
            )}
          </div>
        ),
      },
      {
        accessorKey: 'updated_at',
        header: t('Updated at'),
        cell: ({ row }) => (
          <TimestampCell
            timestamp={row.original.updated_at}
            justNowLabel={t('Just now')}
            format='absolute'
          />
        ),
      },
    ],
    [t]
  )
  const { table } = useDataTable({
    data: list.data?.items ?? [],
    columns,
    pagination,
    onPaginationChange: setPagination,
    globalFilter: keyword,
    columnFilters: [],
    onGlobalFilterChange: (value) => {
      setKeyword(value)
      setPagination((previous) => ({ ...previous, pageIndex: 0 }))
    },
    manualPagination: true,
    manualFiltering: true,
    enableSorting: false,
    enableRowSelection: false,
    totalCount: list.data?.total ?? 0,
    ensurePageInRange: (pages) =>
      setPagination((previous) =>
        previous.pageIndex < Math.max(1, pages)
          ? previous
          : { ...previous, pageIndex: Math.max(0, pages - 1) }
      ),
  })

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Support tickets')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            disabled={list.isFetching || counts.isFetching}
            onClick={() => {
              void list.refetch()
              void counts.refetch()
            }}
          >
            <RefreshCw aria-hidden='true' />
            {t('Refresh')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          {list.isError ? (
            <ErrorState
              description={t('Tickets could not be loaded.')}
              onRetry={() => void list.refetch()}
            />
          ) : (
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={list.isLoading}
              isFetching={list.isFetching}
              emptyTitle={t('No tickets yet.')}
              toolbarProps={{
                searchPlaceholder: t('Search by subject'),
                searchDebounceMs: 400,
                hideViewOptions: true,
                additionalSearch: (
                  <NativeSelect
                    aria-label={t('Ticket queue')}
                    value={filter}
                    onChange={(event) => {
                      setFilter(event.target.value as TicketFilter)
                      setPagination((previous) => ({
                        ...previous,
                        pageIndex: 0,
                      }))
                    }}
                  >
                    <NativeSelectOption value='all'>
                      {t('All')}
                    </NativeSelectOption>
                    <NativeSelectOption value='unread'>
                      {t('Unread')}
                      {counts.data
                        ? ` (${formatNumber(counts.data.unread_count, locale)})`
                        : ''}
                    </NativeSelectOption>
                    <NativeSelectOption value='unreplied'>
                      {t('Awaiting reply')}
                      {counts.data
                        ? ` (${formatNumber(counts.data.unreplied_count, locale)})`
                        : ''}
                    </NativeSelectOption>
                  </NativeSelect>
                ),
                hasAdditionalFilters: filter !== 'all',
                onReset: () => {
                  setFilter('all')
                  setPagination((previous) => ({ ...previous, pageIndex: 0 }))
                },
              }}
            />
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>
      {selectedId !== null && (
        <TicketDialog
          key={selectedId}
          id={selectedId}
          onClose={() => setSelectedId(null)}
        />
      )}
    </>
  )
}
