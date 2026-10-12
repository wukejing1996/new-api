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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { SettingsControlGroup } from '@/features/system-settings/components/settings-form-layout'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import type {
  RegistrationRiskFormValues,
  RegistrationRiskSettings,
} from '../lib/schema'
import { RiskSwitch } from './risk-fields'

export function RiskProviders(props: {
  settings: RegistrationRiskSettings
  disabled: boolean
}) {
  const { t } = useTranslation()
  const form = useFormContext<RegistrationRiskFormValues>()
  const providers = [
    {
      name: 'api_key',
      toggle: 'proxycheck_enabled',
      label: 'proxycheck.io',
      configured: props.settings.api_key_configured,
      description: t(
        'Proxy, VPN, Tor, hosting, scraper, risk score and disposable email checks.'
      ),
    },
    {
      name: 'ipapi_key',
      toggle: 'ipapi_enabled',
      label: 'ipapi.is',
      configured: props.settings.ipapi_key_configured,
      description: t(
        'Proxy, VPN, Tor, datacenter and known abusive IP checks.'
      ),
    },
    {
      name: 'vpnapi_key',
      toggle: 'vpnapi_enabled',
      label: 'VPNAPI.io',
      configured: props.settings.vpnapi_key_configured,
      description: t(
        'Proxy, VPN, Tor and privacy relay checks. Residential proxy and crawler checks depend on the returned fields.'
      ),
    },
  ] as const
  return (
    <SettingsSection title={t('Intelligence providers')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Any enabled provider can flag an account. All enabled providers must complete their required checks before an assessment is considered complete.'
        )}
      </p>
      <div className='grid min-w-0 gap-4 xl:grid-cols-3'>
        {providers.map((provider) => (
          <SettingsControlGroup
            key={provider.name}
            role='group'
            aria-label={provider.label}
          >
            <h4 className='font-medium'>{provider.label}</h4>
            <RiskSwitch
              name={provider.toggle}
              label={t('Enable {{provider}}', { provider: provider.label })}
              disabled={props.disabled}
            />
            <FormField
              control={form.control}
              name={provider.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('{{provider}} API key', { provider: provider.label })}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      value={field.value ?? ''}
                      type='password'
                      autoComplete='new-password'
                      maxLength={256}
                      disabled={props.disabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {provider.configured
                      ? t(
                          'API key configured. Leave blank to keep the saved key.'
                        )
                      : t(
                          'Enter an API key before enabling IP or email checks.'
                        )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <p className='text-muted-foreground text-xs'>
              {provider.description}
            </p>
          </SettingsControlGroup>
        ))}
      </div>
    </SettingsSection>
  )
}
