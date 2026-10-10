import { useState } from 'react'
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
import type { Control, FieldArrayWithId } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { safeNumberFieldProps } from '../utils/numeric-field'
import type { UserGroupRateLimitFormValues } from './lib/user-group-rate-limit'
import { UserGroupRateLimitResponseDialog } from './user-group-rate-limit-response-dialog'

type Props = {
  control: Control<UserGroupRateLimitFormValues>
  fields: FieldArrayWithId<UserGroupRateLimitFormValues, 'rules'>[]
  groups: string[]
  disabled: boolean
  onRemove: (index: number) => void
}

export function UserGroupRateLimitEditor(props: Props) {
  const { t } = useTranslation()
  const [responseRowId, setResponseRowId] = useState<string | null>(null)
  return (
    <StaticDataTable
      className='min-w-0'
      data={props.fields}
      getRowKey={(row) => row.id}
      tableClassName='min-w-[700px]'
      emptyContent={t('No user group rate limits configured.')}
      columns={[
        {
          id: 'group',
          header: t('User Group'),
          cell: (_, index) => (
            <FormField
              control={props.control}
              name={`rules.${index}.group`}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Combobox
                      options={props.groups.map((group) => ({
                        label: group,
                        value: group,
                      }))}
                      value={field.value}
                      onValueChange={(value) => field.onChange(value ?? '')}
                      onBlur={field.onBlur}
                      disabled={props.disabled}
                      aria-label={t('User Group')}
                      placeholder={t('Select group')}
                      className='min-w-40'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ),
        },
        {
          id: 'period',
          header: t('Limit Period'),
          cell: (_, index) => (
            <FormField
              control={props.control}
              name={`rules.${index}.period`}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      aria-label={t('Limit Period')}
                      disabled={props.disabled}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ),
        },
        {
          id: 'unit',
          header: t('Period Unit'),
          cell: (_, index) => (
            <FormField
              control={props.control}
              name={`rules.${index}.unit`}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Combobox
                      options={[
                        { value: 'seconds', label: t('Seconds') },
                        { value: 'minutes', label: t('Minutes') },
                        { value: 'hours', label: t('Hours') },
                        { value: 'days', label: t('Days') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={props.disabled}
                      aria-label={t('Period Unit')}
                      className='min-w-28'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ),
        },
        {
          id: 'requests',
          header: t('Max Requests (incl. failures)'),
          cell: (_, index) => (
            <FormField
              control={props.control}
              name={`rules.${index}.maxRequests`}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={10000}
                      step={1}
                      aria-label={t('Max Requests (incl. failures)')}
                      disabled={props.disabled}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ),
        },
        {
          id: 'customResponse',
          header: t('Custom over-limit reply'),
          cell: (row, index) => (
            <FormField
              control={props.control}
              name={`rules.${index}.customResponseMessage`}
              render={() => (
                <FormItem>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={props.disabled}
                    onClick={() => setResponseRowId(row.id)}
                  >
                    {t('Configure reply')}
                  </Button>
                  <FormMessage />
                  {responseRowId === row.id && (
                    <UserGroupRateLimitResponseDialog
                      control={props.control}
                      index={index}
                      disabled={props.disabled}
                      onClose={() => setResponseRowId(null)}
                    />
                  )}
                </FormItem>
              )}
            />
          ),
        },
        {
          id: 'actions',
          header: t('Actions'),
          cell: (_, index) => (
            <Button
              type='button'
              variant='outline'
              disabled={props.disabled}
              onClick={() => props.onRemove(index)}
            >
              {t('Delete')}
            </Button>
          ),
        },
      ]}
    />
  )
}
