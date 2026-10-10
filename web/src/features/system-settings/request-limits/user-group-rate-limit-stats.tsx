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
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getUserGroupRateLimitStats } from '../api'

export function UserGroupRateLimitStats() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const statsQuery = useQuery({
    queryKey: ['user-group-rate-limit-stats'],
    queryFn: async () =>
      requireServerSuccess(await getUserGroupRateLimitStats()).data,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })
  const counts = statsQuery.data?.counts ?? {}
  const rejections = statsQuery.data?.rejected_counts ?? {}
  const rows = [
    ...new Set([...Object.keys(counts), ...Object.keys(rejections)]),
  ].map((group) => ({
    group,
    count: counts[group] ?? 0,
    rejected: rejections[group] ?? 0,
  }))
  rows.sort(
    (a, b) =>
      b.count + b.rejected - (a.count + a.rejected) ||
      a.group.localeCompare(b.group, locale)
  )
  const total = rows.reduce((sum, row) => sum + row.count, 0)
  const totalRejected = rows.reduce((sum, row) => sum + row.rejected, 0)
  return (
    <section
      className='min-w-0 space-y-3 pt-4'
      aria-label={t('Rate limit counts')}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='font-medium'>{t('Rate limit counts')}</h3>
        <Button
          type='button'
          variant='outline'
          disabled={statsQuery.isFetching}
          onClick={() => void statsQuery.refetch()}
        >
          {t('Refresh counts')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Cumulative custom replies and 429 rejections by user group. Counts are independent of the rate limit window and include previously configured groups.'
        )}
      </p>
      {statsQuery.isPending && (
        <LoadingState inline message={t('Loading...')} />
      )}
      {statsQuery.isError && (
        <ErrorState
          title={t('Failed to load rate limit counts')}
          onRetry={() => void statsQuery.refetch()}
        />
      )}
      {!statsQuery.isError &&
        statsQuery.data &&
        (statsQuery.data.redis_enabled ? (
          <>
            <p role='status'>
              {t('Total successful custom replies')}:{' '}
              {formatNumber(total, locale)}
            </p>
            <p role='status'>
              {t('Total 429 rejections')}: {formatNumber(totalRejected, locale)}
            </p>
            <StaticDataTable
              className='min-w-0'
              data={rows}
              getRowKey={(row) => row.group}
              emptyContent={t('No rate limit events counted yet.')}
              columns={[
                {
                  id: 'group',
                  header: t('User Group'),
                  cell: (row) => row.group,
                },
                {
                  id: 'count',
                  header: t('Successful custom replies'),
                  cell: (row) => formatNumber(row.count, locale),
                },
                {
                  id: 'rejected',
                  header: t('429 rejections'),
                  cell: (row) => formatNumber(row.rejected, locale),
                },
              ]}
            />
          </>
        ) : (
          <p role='status'>
            {t('Redis is not enabled. Rate limit counts are unavailable.')}
          </p>
        ))}
    </section>
  )
}
