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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { getGroups } from '@/features/users/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  createUserGroupRateLimitSchema,
  parseUserGroupRateLimit,
  serializeUserGroupRateLimit,
  type UserGroupRateLimitFormValues,
} from './lib/user-group-rate-limit'
import { UserGroupRateLimitEditor } from './user-group-rate-limit-editor'

export function UserGroupRateLimitSection(props: { defaultValue: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const parsed = useMemo(() => {
    try {
      return parseUserGroupRateLimit(props.defaultValue)
    } catch {
      return null
    }
  }, [props.defaultValue])
  const form = useForm<UserGroupRateLimitFormValues>({
    resolver: zodResolver(createUserGroupRateLimitSchema(t)),
    defaultValues: parsed ?? { enabled: false, rules: [] },
    mode: 'onChange',
  })
  const rules = useFieldArray({ control: form.control, name: 'rules' })
  const groupsQuery = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
    staleTime: 5 * 60 * 1000,
  })
  useEffect(() => {
    if (parsed) {
      form.reset(parsed)
    }
  }, [parsed, form])
  const availableGroups = [
    ...new Set([
      ...(groupsQuery.data?.data ?? []),
      ...rules.fields.map((rule) => rule.group).filter(Boolean),
    ]),
  ]
  const onSubmit = async (values: UserGroupRateLimitFormValues) => {
    const value = serializeUserGroupRateLimit(values)
    try {
      await updateOption.mutateAsync({ key: 'UserGroupRateLimit', value })
      form.reset(values)
    } catch {
      // The shared mutation displays the error; keep unsaved edits for retry.
    }
  }

  if (!parsed) {
    return <ErrorState title={t('Failed to load user group rate limits')} />
  }

  return (
    <SettingsSection title={t('User Group Rate Limits')}>
      <Form {...form}>
        <SettingsForm
          className='grid-cols-1'
          onSubmit={form.handleSubmit(onSubmit)}
        >
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            isSaveDisabled={!form.formState.isDirty}
            saveLabel='Save user group rate limits'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable user group rate limits')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Applies to the account user group, including Auto keys. Matching rules override the existing model rate limits; other users keep the existing behavior.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Each user has a separate quota shared by all keys and models. Requests count before forwarding, including failures. Over-limit requests return 429, or a custom reply with HTTP 200 when enabled. They never call upstream or create usage logs.'
            )}
          </p>
          {groupsQuery.isError && (
            <ErrorState
              title={t('Failed to load groups')}
              onRetry={() => void groupsQuery.refetch()}
            />
          )}
          <div className='min-w-0 space-y-4'>
            <Button
              type='button'
              variant='outline'
              disabled={
                groupsQuery.isLoading ||
                groupsQuery.isError ||
                updateOption.isPending ||
                rules.fields.length >= 1000
              }
              onClick={() =>
                rules.append({
                  group: '',
                  period: 1,
                  unit: 'hours',
                  maxRequests: 1,
                  customResponseEnabled: false,
                  customResponseMessage: '',
                })
              }
            >
              {t('Add group')}
            </Button>
            <UserGroupRateLimitEditor
              control={form.control}
              fields={rules.fields}
              groups={availableGroups}
              disabled={updateOption.isPending}
              onRemove={rules.remove}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
