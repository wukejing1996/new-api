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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { AuditDetailValue } from '@/features/usage-logs/audit/components/audit-detail-value'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import { toIntlLocale } from '@/i18n/languages'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getRegistrationRiskRecords } from '../api'
import {
  readRiskRecord,
  riskOutcomeLabel,
  riskReasonLabel,
  type RiskLog,
  type RiskRecordFilters,
} from '../lib/records'

const emptyRecords: RiskLog[] = []

function RiskRecordDetails(props: { entry: RiskLog }) {
  const { t } = useTranslation()
  const record = readRiskRecord(props.entry)
  const yesNo = (value: boolean | undefined) => {
    if (value === undefined) return t('Not recorded')
    return value ? t('Yes') : t('No')
  }
  return (
    <Dialog
      title={t('Registration risk assessment details')}
      trigger={
        <Button variant='ghost' size='sm'>
          {t('Details')}
        </Button>
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
      contentClassName='min-w-0 sm:max-w-xl'
    >
      <DetailSection label={t('User')}>
        <DetailRow label={t('Username')} value={props.entry.username} />
        <DetailRow
          label={t('User ID')}
          value={props.entry.user_id || t('Account creation failed')}
        />
        <DetailRow
          label={t('Time')}
          value={dayjs
            .unix(props.entry.created_at)
            .format('YYYY-MM-DD HH:mm:ss')}
        />
        <DetailRow label='IP' value={props.entry.ip} />
        <DetailRow label={t('Client')} value={props.entry.user_agent || '—'} />
        <DetailRow label={t('Request ID')} value={props.entry.request_id} />
      </DetailSection>
      {record ? (
        <>
          <DetailSection label={t('Risk assessment')}>
            <DetailRow
              label={t('Assessment source')}
              value={
                record.source === 'association'
                  ? t('Related account restriction')
                  : t('New registration')
              }
            />
            {record.source === 'association' && (
              <>
                <DetailRow
                  label={t('Trigger user ID')}
                  value={record.trigger_user_id}
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'The IP and request above belong to the triggering registration. This account was restricted by association, without a new intelligence query.'
                  )}
                </p>
              </>
            )}
            <DetailRow
              label={t('Restricted')}
              value={yesNo(record.assessment.restricted)}
            />
            <DetailRow
              label={t('Risk assessment incomplete')}
              value={yesNo(record.assessment.incomplete)}
            />
            <DetailRow
              label={t('Matched risk signals')}
              value={
                record.assessment.reasons
                  .map((reason) => riskReasonLabel(reason, t))
                  .join(' · ') || t('No enabled risk rule matched')
              }
            />
            {record.source !== 'association' && (
              <DetailRow
                label={t('Disposable email')}
                value={yesNo(record.assessment.disposable)}
              />
            )}
            {record.assessment.ip_result && (
              <DetailSection label={t('IP intelligence result')}>
                <AuditDetailValue
                  label={t('IP intelligence result')}
                  value={JSON.stringify(record.assessment.ip_result, null, 2)}
                  mono
                />
              </DetailSection>
            )}
          </DetailSection>
          <DetailSection label={t('Processing result')}>
            <DetailRow
              label={t('Result')}
              value={riskOutcomeLabel(record.outcome, t)}
            />
            <DetailRow
              label={t('Original group')}
              value={record.original_group}
            />
            <DetailRow
              label={t('Final group')}
              value={record.final_group ?? t('Not recorded')}
            />
            <DetailRow
              label={t('Group update applied')}
              value={yesNo(record.group_applied)}
            />
            <DetailRow
              label={t('Cache synchronization completed')}
              value={yesNo(record.cache_synced)}
            />
            <DetailRow
              label={t('Related user IDs')}
              value={record.related_user_ids?.join(', ') || '—'}
            />
            {record.processing_error && (
              <DetailRow
                label={t('Processing error')}
                value={record.processing_error}
              />
            )}
          </DetailSection>
          <DetailSection label={t('Policy snapshot')}>
            <AuditDetailValue
              label={t('Policy snapshot')}
              value={JSON.stringify(record.policy, null, 2)}
              mono
            />
          </DetailSection>
        </>
      ) : (
        <p>{t('Audit metadata is unavailable')}</p>
      )}
    </Dialog>
  )
}

export function RiskRecords() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [filters, setFilters] = useState<RiskRecordFilters>({
    p: 1,
    page_size: 20,
  })
  const invalidRange =
    filters.start_timestamp !== undefined &&
    filters.end_timestamp !== undefined &&
    filters.start_timestamp > filters.end_timestamp
  const query = useQuery({
    queryKey: ['registration-risk-records', userId, filters],
    queryFn: () => getRegistrationRiskRecords(filters),
    retry: false,
    enabled: !invalidRange,
  })
  const columns = useMemo<ColumnDef<RiskLog>[]>(
    () => [
      {
        accessorKey: 'created_at',
        header: t('Time'),
        cell: ({ row }) =>
          dayjs.unix(row.original.created_at).format('YYYY-MM-DD HH:mm:ss'),
        meta: { label: t('Time') },
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) =>
          `${row.original.username} (ID: ${row.original.user_id})`,
        meta: { label: t('User'), mobileTitle: true },
      },
      { accessorKey: 'ip', header: 'IP', meta: { label: 'IP' } },
      {
        id: 'reasons',
        header: t('Matched risk signals'),
        cell: ({ row }) => {
          const record = readRiskRecord(row.original)
          if (!record) return t('Not recorded')
          return (
            record.assessment.reasons
              .map((reason) => riskReasonLabel(reason, t))
              .join(' · ') || t('No enabled risk rule matched')
          )
        },
        meta: { label: t('Matched risk signals') },
      },
      {
        id: 'score',
        header: t('IP risk score'),
        cell: ({ row }) => {
          const score = readRiskRecord(row.original)?.assessment.ip_result
            ?.detections.risk
          return typeof score === 'number' ? formatNumber(score, locale) : '—'
        },
        meta: { label: t('IP risk score') },
      },
      {
        id: 'outcome',
        header: t('Processing result'),
        cell: ({ row }) =>
          riskOutcomeLabel(readRiskRecord(row.original)?.outcome, t),
        meta: { label: t('Processing result') },
      },
      {
        id: 'details',
        header: t('Details'),
        cell: ({ row }) => <RiskRecordDetails entry={row.original} />,
        meta: { label: t('Details') },
      },
    ],
    [t, locale]
  )
  const { table } = useDataTable({
    columns,
    data: query.isError ? emptyRecords : (query.data?.items ?? emptyRecords),
    getRowId: (entry) => entry.event_id,
    totalCount: query.isError ? 0 : (query.data?.total ?? 0),
    pagination: { pageIndex: filters.p - 1, pageSize: filters.page_size },
    onPaginationChange: (updater) => {
      if (query.isFetching || query.isError || invalidRange) return
      setFilters((previous) => {
        const current = {
          pageIndex: previous.p - 1,
          pageSize: previous.page_size,
        }
        const next = typeof updater === 'function' ? updater(current) : updater
        return {
          ...previous,
          p: next.pageSize === previous.page_size ? next.pageIndex + 1 : 1,
          page_size: next.pageSize,
        }
      })
    },
    enableRowSelection: false,
    enableSorting: false,
    manualFiltering: true,
    manualPagination: true,
  })
  const update = (patch: Partial<typeof filters>) =>
    setFilters((previous) => ({ ...previous, ...patch, p: 1 }))
  return (
    <SettingsSection title={t('Registration risk records')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Records all assessments while risk control is enabled, including clean registrations and processing failures. Historical records remain available after disabling risk control.'
        )}
      </p>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={query.isPending && !invalidRange}
        isFetching={query.isFetching}
        className='h-auto min-h-0'
        paginationInFooter={false}
        emptyTitle={
          query.isError ? t('Failed to load audit records') : t('No records')
        }
        toolbar={
          <div className='space-y-3'>
            <LogsFilterToolbar
              table={table}
              hasActiveFilters={
                !!(
                  filters.username ||
                  filters.user_id ||
                  filters.success ||
                  filters.start_timestamp ||
                  filters.end_timestamp
                )
              }
              searchLoading={query.isFetching}
              onSearch={() => {
                if (!invalidRange) void query.refetch()
              }}
              onReset={() => setFilters({ p: 1, page_size: filters.page_size })}
              primaryFilters={
                <>
                  <LogsFilterField>
                    <LogsFilterInput
                      aria-label={t('Username')}
                      placeholder={t('Username')}
                      value={filters.username ?? ''}
                      maxLength={64}
                      onChange={(event) =>
                        update({ username: event.target.value || undefined })
                      }
                    />
                  </LogsFilterField>
                  <LogsFilterField>
                    <LogsFilterInput
                      aria-label={t('User ID')}
                      placeholder={t('User ID')}
                      type='number'
                      min={1}
                      step={1}
                      value={filters.user_id ?? ''}
                      onChange={(event) =>
                        update({
                          user_id: event.target.value
                            ? Number(event.target.value)
                            : undefined,
                        })
                      }
                    />
                  </LogsFilterField>
                  <LogsFilterField>
                    <NativeSelect
                      aria-label={t('Processing result')}
                      value={filters.success ?? ''}
                      onChange={(event) =>
                        update({ success: event.target.value || undefined })
                      }
                    >
                      <NativeSelectOption value=''>
                        {t('All results')}
                      </NativeSelectOption>
                      <NativeSelectOption value='true'>
                        {t('Success')}
                      </NativeSelectOption>
                      <NativeSelectOption value='false'>
                        {t('Failed')}
                      </NativeSelectOption>
                    </NativeSelect>
                  </LogsFilterField>
                  <LogsFilterField wide>
                    <CompactDateTimeRangePicker
                      start={
                        filters.start_timestamp === undefined
                          ? undefined
                          : new Date(filters.start_timestamp * 1000)
                      }
                      end={
                        filters.end_timestamp === undefined
                          ? undefined
                          : new Date(filters.end_timestamp * 1000)
                      }
                      onChange={({ start, end }) =>
                        update({
                          start_timestamp: start
                            ? Math.floor(start.getTime() / 1000)
                            : undefined,
                          end_timestamp: end
                            ? Math.floor(end.getTime() / 1000)
                            : undefined,
                        })
                      }
                    />
                  </LogsFilterField>
                </>
              }
            />
            {invalidRange && (
              <p role='alert' className='text-destructive text-sm'>
                {t('End time must be after start time')}
              </p>
            )}
            {query.isError && (
              <ErrorState
                title={t('Failed to load audit records')}
                onRetry={() => void query.refetch()}
              />
            )}
          </div>
        }
      />
    </SettingsSection>
  )
}
