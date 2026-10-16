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
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatDateTimeStr } from '@/lib/format'
import { cn } from '@/lib/utils'

interface LogStatsRefreshControlsProps {
  refreshInterval: string
  onIntervalChange: (value: string) => void
  onRefresh: () => void
  isFetching: boolean
  isError: boolean
  dataUpdatedAt: number
}

export function LogStatsRefreshControls(props: LogStatsRefreshControlsProps) {
  const { t } = useTranslation()
  const items = [
    { value: '0', label: t('Auto refresh: off') },
    ...[10, 30, 60].map((seconds) => ({
      value: String(seconds),
      label: t('Every {{seconds}} seconds', { seconds }),
    })),
  ]
  const lastUpdated =
    props.dataUpdatedAt > 0
      ? `${t('Last updated:')} ${formatDateTimeStr(new Date(props.dataUpdatedAt))}`
      : undefined

  return (
    <div className='flex max-w-full flex-wrap items-center gap-2'>
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        className='max-sm:size-11'
        aria-label={t('Refresh statistics')}
        aria-busy={props.isFetching}
        title={lastUpdated}
        disabled={props.isFetching}
        onClick={props.onRefresh}
      >
        <RefreshCw
          aria-hidden='true'
          className={cn(
            props.isFetching && 'animate-spin motion-reduce:animate-none'
          )}
        />
      </Button>
      <Select
        items={items}
        value={props.refreshInterval}
        onValueChange={(value) => props.onIntervalChange(value ?? '0')}
      >
        <SelectTrigger
          size='sm'
          className='max-sm:h-11'
          aria-label={t('Auto refresh')}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent align='end'>
          <SelectGroup>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {props.isError && (
        <span
          role='status'
          className='text-destructive text-xs'
          title={lastUpdated}
        >
          {t('Refresh failed')}
        </span>
      )}
    </div>
  )
}
