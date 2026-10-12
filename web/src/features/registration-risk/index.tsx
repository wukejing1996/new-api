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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsFormGrid,
} from '@/features/system-settings/components/settings-form-layout'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { getGroups } from '@/features/users/api'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import {
  getRegistrationRiskSettings,
  saveRegistrationRiskSettings,
} from './api'
import { RiskNumber, RiskSwitch } from './components/risk-fields'
import { RiskProviders } from './components/risk-providers'
import { RiskQueryTest } from './components/risk-query-test'
import { RiskRecords } from './components/risk-records'
import {
  createRegistrationRiskFormSchema,
  type RegistrationRiskFormValues,
} from './lib/schema'

const queryKey = ['registration-risk-settings']

export function RegistrationRisk() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const settings = useQuery({ queryKey, queryFn: getRegistrationRiskSettings })
  const groups = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
  })
  const form = useForm<RegistrationRiskFormValues>({
    resolver: zodResolver(createRegistrationRiskFormSchema(t)),
    mode: 'onChange',
  })
  const save = useMutation({
    mutationFn: (values: RegistrationRiskFormValues) => {
      const { api_key, ipapi_key, vpnapi_key, ...config } = values
      return saveRegistrationRiskSettings(
        config,
        api_key,
        ipapi_key,
        vpnapi_key
      )
    },
    onSuccess: (data) => {
      client.setQueryData(queryKey, data)
      form.reset({ ...data.config, api_key: '', ipapi_key: '', vpnapi_key: '' })
      toast.success(t('Settings saved'))
    },
  })
  useEffect(() => {
    if (settings.data && !form.formState.isDirty) {
      form.reset({
        ...settings.data.config,
        api_key: '',
        ipapi_key: '',
        vpnapi_key: '',
      })
    }
  }, [settings.data, form])
  const target = form.watch('target_group')
  const apiConfigured = settings.data?.api_key_configured ?? false
  const testConfigured = Boolean(
    ((settings.data?.config.proxycheck_enabled ?? true) && apiConfigured) ||
    (settings.data?.config.ipapi_enabled &&
      settings.data.ipapi_key_configured) ||
    (settings.data?.config.vpnapi_enabled &&
      settings.data.vpnapi_key_configured)
  )
  const disabled = save.isPending
  let content
  if (settings.isPending) {
    content = <LoadingState />
  } else if (settings.isError || !settings.data) {
    content = (
      <ErrorState
        title={t('Failed to load risk settings')}
        onRetry={() => void settings.refetch()}
      />
    )
  } else {
    content = (
      <>
        <Form {...form}>
          <SettingsForm
            onSubmit={form.handleSubmit((values) => save.mutate(values))}
          >
            <p className='text-muted-foreground text-sm'>
              {t(
                'Any enabled risk rule places new accounts in the selected group. IP and subnet association rules also move all related accounts within the window. Existing logins and administrator-created accounts are excluded.'
              )}
            </p>
            <SettingsControlGroup>
              <RiskSwitch
                name='enabled'
                label={t('Enable new user risk control')}
                disabled={disabled}
              />
              <FormField
                control={form.control}
                name='target_group'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Risk user group')}</FormLabel>
                    <FormControl>
                      <NativeSelect
                        {...field}
                        value={field.value ?? ''}
                        disabled={
                          disabled || groups.isPending || groups.isError
                        }
                        className='w-full'
                      >
                        <NativeSelectOption value=''>
                          {t('Select a risk user group')}
                        </NativeSelectOption>
                        {[
                          ...new Set([
                            ...(groups.data?.data ?? []),
                            ...(target ? [target] : []),
                          ]),
                        ].map((group) => (
                          <NativeSelectOption key={group} value={group}>
                            {group}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Enable a user group rate limit for this group first. Set its maximum requests to 0 to prevent upstream calls.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {groups.isError && (
                <ErrorState
                  title={t('Failed to load groups')}
                  onRetry={() => void groups.refetch()}
                />
              )}
              {target === settings.data.config.target_group && target && (
                <p className='text-muted-foreground text-sm'>
                  {settings.data.target_limit_enabled
                    ? t('The selected group has an active rate limit.')
                    : t('The selected group has no active rate limit.')}
                </p>
              )}
            </SettingsControlGroup>
            {!settings.data.redis_enabled && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Redis is required for IP and subnet association checks.')}
              </p>
            )}
            <RiskProviders settings={settings.data} disabled={disabled} />
            <SettingsControlGroup>
              <SettingsSection title={t('IP and email risk rules')}>
                <RiskSwitch
                  name='proxy'
                  label={t('Proxy IP')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='vpn'
                  label={t('VPN IP')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='tor'
                  label={t('Tor exit node')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='hosting'
                  label={t('Hosting or datacenter IP')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='scraper'
                  label={t('Known scraper IP')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='risk_score_enabled'
                  label={t('IP risk score threshold')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='abuser'
                  label={t('Known abusive IP')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='relay'
                  label={t('Privacy relay IP')}
                  disabled={disabled}
                />
                <RiskNumber
                  name='risk_threshold'
                  label={t('Risk score at or above')}
                  min={0}
                  max={100}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='disposable_email'
                  label={t('Disposable email')}
                  disabled={disabled}
                />
                <RiskSwitch
                  name='fail_closed'
                  label={t(
                    'Restrict accounts when required checks cannot complete'
                  )}
                  disabled={disabled}
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Timeouts, exhausted API quotas and missing required data count as incomplete assessments. CAPTCHA expiry requires re-verification and is not a risk signal.'
                  )}
                </p>
              </SettingsSection>
            </SettingsControlGroup>
            <SettingsControlGroup>
              <SettingsSection title={t('Related registrations')}>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Counts successful accounts only. Reaching the trigger moves every related account in the window, including the first account. Groups are not restored when the window expires.'
                  )}
                </p>
                <RiskSwitch
                  name='ip_rule.enabled'
                  label={t('Detect registrations from the same IP')}
                  disabled={disabled}
                />
                <SettingsFormGrid>
                  <RiskNumber
                    name='ip_rule.window_seconds'
                    label={t('IP window (seconds)')}
                    min={1}
                    max={86400}
                    disabled={disabled}
                  />
                  <RiskNumber
                    name='ip_rule.trigger_count'
                    label={t('IP account trigger count')}
                    min={2}
                    max={100}
                    disabled={disabled}
                  />
                </SettingsFormGrid>
                <RiskSwitch
                  name='subnet_rule.enabled'
                  label={t('Detect registrations from the same subnet')}
                  disabled={disabled}
                />
                <SettingsFormGrid>
                  <RiskNumber
                    name='subnet_rule.window_seconds'
                    label={t('Subnet window (seconds)')}
                    min={1}
                    max={86400}
                    disabled={disabled}
                  />
                  <RiskNumber
                    name='subnet_rule.trigger_count'
                    label={t('Subnet account trigger count')}
                    min={2}
                    max={100}
                    disabled={disabled}
                  />
                  <RiskNumber
                    name='ipv4_prefix'
                    label={t('IPv4 prefix length')}
                    min={16}
                    max={32}
                    disabled={disabled}
                  />
                  <RiskNumber
                    name='ipv6_prefix'
                    label={t('IPv6 prefix length')}
                    min={32}
                    max={128}
                    disabled={disabled}
                  />
                </SettingsFormGrid>
              </SettingsSection>
            </SettingsControlGroup>
            <SettingsFormGrid>
              <RiskNumber
                name='timeout_seconds'
                label={t('Assessment timeout (seconds)')}
                min={1}
                max={15}
                disabled={disabled}
              />
              <RiskNumber
                name='cache_seconds'
                label={t('Intelligence cache (seconds, 0 disables)')}
                min={0}
                max={3600}
                disabled={disabled}
              />
            </SettingsFormGrid>
            {save.isError && (
              <p role='alert' className='text-destructive text-sm'>
                {getServerErrorMessage(
                  save.error,
                  t('Failed to save risk settings')
                )}
              </p>
            )}
            <Button
              type='submit'
              variant='secondary'
              className='w-fit'
              disabled={disabled || !form.formState.isDirty || groups.isError}
            >
              {disabled ? t('Saving...') : t('Save risk settings')}
            </Button>
          </SettingsForm>
        </Form>
        <RiskQueryTest configured={testConfigured} />
        <RiskRecords />
      </>
    )
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('New User Risk Control')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-4xl space-y-8 p-3 sm:p-5'>
          {content}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
