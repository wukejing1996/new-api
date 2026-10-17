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
import { Switch } from '@/components/ui/switch'

import {
  SettingsControlGroup,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { safeNumberFieldProps } from '../utils/numeric-field'

export type UsdtSettingsValues = {
  UsdtEnabled: boolean
  UsdtBscEnabled: boolean
  UsdtBscReceiveAddress: string
  UsdtBscRpcURL: string
  UsdtMinTopUp: number
  UsdtReceiveAddress: string
  UsdtTrongridApiKey: string
  UsdtNetwork: 'TRC20'
  UsdtCheckInterval: number
  UsdtOrderExpireTime: number
}

export function UsdtSettingsFields() {
  const { t } = useTranslation()
  const form = useFormContext<UsdtSettingsValues>()

  return (
    <div className='min-w-0 space-y-6'>
      <div>
        <h3 className='text-lg font-medium'>{t('USDT Payment')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Configure TRC20 and BSC payments independently. Minimum amount and timing apply to both networks.'
          )}
        </p>
      </div>

      <SettingsControlGroup
        role='group'
        aria-label={t('Shared payment settings')}
        className='space-y-5 p-4 sm:p-5'
      >
        <div>
          <h4 className='font-medium'>{t('Shared payment settings')}</h4>
          <p className='text-muted-foreground text-sm'>
            {t('These settings apply to both TRC20 and BSC.')}
          </p>
        </div>
        <SettingsFormGrid className='xl:grid-cols-3'>
          <FormField
            control={form.control}
            name='UsdtMinTopUp'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum Top-up')} (USDT)</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={10000}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Minimum amount users can top up via USDT')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='UsdtCheckInterval'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Check Interval (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={10}
                    max={3600}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Payment status check interval (minimum 10 seconds)')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='UsdtOrderExpireTime'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Order Expiration (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={300}
                    max={86400}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Order expiration time (default 1800 seconds = 30 minutes)'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsFormGrid>
      </SettingsControlGroup>

      <SettingsControlGroup
        role='group'
        aria-label={t('TRC20 (TRON)')}
        className='space-y-5 p-4 sm:p-5'
      >
        <div>
          <h4 className='font-medium'>{t('TRC20 (TRON)')}</h4>
          <p className='text-muted-foreground text-sm'>
            {t(
              'The address and API key below are used only for TRC20 payments.'
            )}
          </p>
        </div>
        <FormField
          control={form.control}
          name='UsdtEnabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Enable TRC20 USDT')}</FormLabel>
                <FormDescription>
                  {t('Allow users to create new TRC20 payment orders.')}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <SettingsFormGrid>
          <FormField
            control={form.control}
            name='UsdtReceiveAddress'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('TRC20 receive address')}</FormLabel>
                <FormControl>
                  <Input placeholder='T…' autoComplete='off' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Your TRC20 USDT receive address on Tron blockchain')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='UsdtTrongridApiKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('TronGrid API Key (Optional)')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Leave blank to keep the saved key')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'A TronGrid API key is recommended for payment lookups. Check usage and limits in your TronGrid account.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsFormGrid>
      </SettingsControlGroup>

      <SettingsControlGroup
        role='group'
        aria-label={t('BSC (BEP20)')}
        className='space-y-5 p-4 sm:p-5'
      >
        <div>
          <h4 className='font-medium'>{t('BSC (BEP20)')}</h4>
          <p className='text-muted-foreground text-sm'>
            {t(
              'The address and lookup service below are used only for BSC payments.'
            )}
          </p>
        </div>
        <FormField
          control={form.control}
          name='UsdtBscEnabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Enable BSC (BEP20) USDT')}</FormLabel>
                <FormDescription>
                  {t(
                    'Save a BSC address and lookup URL before enabling. The connection is checked when enabled.'
                  )}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <SettingsFormGrid>
          <FormField
            control={form.control}
            name='UsdtBscReceiveAddress'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('BSC receive address')}</FormLabel>
                <FormControl>
                  <Input placeholder='0x…' autoComplete='off' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Use your BSC mainnet address. Never enter a private key or recovery phrase.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='UsdtBscRpcURL'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('BSC payment lookup URL')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder='https://…'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    "Enter your BSC provider's HTTPS RPC URL. Leave blank to keep the saved URL; its capabilities are checked before enabling."
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsFormGrid>
      </SettingsControlGroup>
    </div>
  )
}
