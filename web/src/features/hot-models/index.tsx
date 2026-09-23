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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Flame, RefreshCw, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'

type HotModel = {
  model_name: string
  is_hot: boolean
  created_time: number
}

type HotModelUpdate = Pick<HotModel, 'model_name' | 'is_hot'> & {
  created_time?: number
}

const queryKey = ['hot-models'] as const
const pageSize = 50

export function HotModels() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [hotOnly, setHotOnly] = useState(false)
  const [page, setPage] = useState(0)
  const query = useQuery({
    queryKey,
    queryFn: async () => {
      const response = await api.get<{ success: boolean; data: HotModel[] }>(
        '/api/hot-models/'
      )
      if (!response.data.success) {
        throw new Error(t('Failed to load hot models. Please retry.'))
      }
      return response.data.data
    },
  })
  const mutation = useMutation({
    mutationFn: async (row: HotModelUpdate) => {
      const response = await api.put<{ success: boolean; message?: string }>(
        '/api/hot-models/',
        row
      )
      if (!response.data.success) {
        throw new Error(response.data.message || t('Request failed'))
      }
    },
    onSuccess: async () => {
      toast.success(t('Hot model setting saved'))
      await queryClient.invalidateQueries({ queryKey })
    },
  })
  const models = query.data ?? []
  const filtered = models.filter(
    (row) =>
      row.model_name.toLowerCase().includes(search.trim().toLowerCase()) &&
      (!hotOnly || row.is_hot)
  )
  const lastPage = Math.max(0, Math.ceil(filtered.length / pageSize) - 1)
  const currentPage = Math.min(page, lastPage)
  const visible = filtered.slice(
    currentPage * pageSize,
    (currentPage + 1) * pageSize
  )

  return (
    <section className='space-y-4' aria-label={t('Hot Model Management')}>
      <div className='bg-card rounded-xl border p-5'>
        <div className='flex flex-wrap items-start justify-between gap-4'>
          <div className='space-y-2'>
            <h2 className='flex items-center gap-2 font-semibold'>
              <Flame
                className='size-5 text-orange-600 dark:text-orange-400'
                aria-hidden='true'
              />
              {t('Hot Model Management')}
            </h2>
            <p className='text-muted-foreground max-w-2xl text-sm'>
              {t(
                'Models come from enabled channels. Hot models appear first in the public catalog. Changes save automatically.'
              )}
            </p>
          </div>
          <Button
            variant='outline'
            disabled={query.isFetching || mutation.isPending}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className='size-4' aria-hidden='true' />
            {t('Refresh')}
          </Button>
        </div>
      </div>
      <div className='flex flex-wrap items-center gap-4'>
        <div className='relative w-full sm:max-w-sm'>
          <Search
            className='text-muted-foreground pointer-events-none absolute top-2.5 left-3 size-4'
            aria-hidden='true'
          />
          <Input
            className='pl-9'
            aria-label={t('Search model name')}
            placeholder={t('Search model name')}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              setPage(0)
            }}
          />
        </div>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={hotOnly}
            onCheckedChange={(value) => {
              setHotOnly(value)
              setPage(0)
            }}
          />
          {t('Hot models only')}
        </label>
        <span className='text-muted-foreground text-sm' aria-live='polite'>
          {t('{{count}} models', { count: filtered.length })}
        </span>
      </div>
      {query.isPending && <p role='status'>{t('Loading...')}</p>}
      {query.isError && (
        <p role='alert' className='text-destructive'>
          {t('Failed to load hot models. Please retry.')}
        </p>
      )}
      {!query.isPending && !query.isError && (
        <div className='bg-card overflow-hidden rounded-xl border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model Name')}</TableHead>
                <TableHead>{t('Catalog entry time')}</TableHead>
                <TableHead className='text-right'>{t('Hot Model')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((row) => (
                <TableRow key={row.model_name}>
                  <TableCell className='max-w-[32rem] whitespace-normal'>
                    <span className='font-mono text-sm break-all'>
                      {row.model_name}
                    </span>
                    {row.is_hot && (
                      <Flame
                        className='ml-2 inline size-4 text-orange-600 dark:text-orange-400'
                        aria-label={t('Hot Model')}
                      />
                    )}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    <CatalogTimeEditor
                      timestamp={row.created_time}
                      disabled={mutation.isPending}
                      unknownLabel={t('Unknown')}
                      onSave={(createdTime) =>
                        mutation.mutate({
                          model_name: row.model_name,
                          is_hot: row.is_hot,
                          created_time: createdTime,
                        })
                      }
                    />
                  </TableCell>
                  <TableCell className='text-right'>
                    <Switch
                      aria-label={t('Mark {{model}} as hot', {
                        model: row.model_name,
                      })}
                      checked={row.is_hot}
                      disabled={mutation.isPending}
                      onCheckedChange={(value) =>
                        mutation.mutate({
                          model_name: row.model_name,
                          is_hot: value,
                        })
                      }
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!visible.length && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground py-10 text-center'
                  >
                    {t('No matching enabled models')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}
      <div className='flex items-center justify-between gap-3'>
        <p className='text-muted-foreground max-w-xl text-xs'>
          {t(
            'Historical entry times are unknown. New models are dated when first added to a channel; editing or marking hot keeps that date.'
          )}
        </p>
        <div className='flex shrink-0 items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={currentPage === 0}
            onClick={() => setPage(currentPage - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-sm'>
            {currentPage + 1} / {lastPage + 1}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={currentPage === lastPage}
            onClick={() => setPage(currentPage + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </section>
  )
}

function CatalogTimeEditor(props: {
  timestamp: number
  disabled: boolean
  unknownLabel: string
  onSave: (timestamp: number) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(() => timestampToInput(props.timestamp))

  useEffect(() => {
    setDraft(timestampToInput(props.timestamp))
  }, [props.timestamp])

  if (!editing) {
    return (
      <button
        type='button'
        className='hover:text-foreground inline-flex items-center border-b border-dashed border-current/40 py-1 tabular-nums transition-colors'
        disabled={props.disabled}
        onClick={() => setEditing(true)}
        title={props.unknownLabel}
      >
        {props.timestamp > 0
          ? formatTimestampToDate(props.timestamp)
          : props.unknownLabel}
      </button>
    )
  }

  return (
    <Input
      autoFocus
      type='datetime-local'
      className='h-8 w-[12.5rem] text-xs tabular-nums'
      value={draft}
      disabled={props.disabled}
      aria-label={props.unknownLabel}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => {
        const nextTimestamp = parseTimestampInput(draft)
        setEditing(false)
        if (nextTimestamp !== props.timestamp) props.onSave(nextTimestamp)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          setDraft(timestampToInput(props.timestamp))
          setEditing(false)
        }
      }}
    />
  )
}

function timestampToInput(timestamp: number) {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function parseTimestampInput(value: string) {
  if (!value) return 0
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? Math.floor(timestamp / 1000) : 0
}
