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
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import type { UserGroupRateLimitFormValues } from './lib/user-group-rate-limit'

type Props = {
  control: Control<UserGroupRateLimitFormValues>
  index: number
  disabled: boolean
  onClose: () => void
}

export function UserGroupRateLimitResponseDialog(props: Props) {
  const { t } = useTranslation()
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Custom over-limit reply')}
      description={t(
        'Within the limit, requests proceed normally. After the limit is exceeded, enabling this option returns your text as a model reply with HTTP 200 instead of 429. Click Save on the settings page to apply changes.'
      )}
      bodyClassName='space-y-4'
      footer={
        <Button type='button' onClick={props.onClose}>
          {t('Done')}
        </Button>
      }
    >
      <FormField
        control={props.control}
        name={`rules.${props.index}.customResponseEnabled`}
        render={({ field }) => (
          <SettingsSwitchItem>
            <SettingsSwitchContent>
              <FormLabel>{t('Enable custom over-limit reply')}</FormLabel>
            </SettingsSwitchContent>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                disabled={props.disabled}
              />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />
      <FormField
        control={props.control}
        name={`rules.${props.index}.customResponseMessage`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Custom reply text')}</FormLabel>
            <FormControl>
              <Textarea {...field} disabled={props.disabled} rows={5} />
            </FormControl>
            <FormDescription>
              {t(
                'Enter up to 4000 characters. Chat APIs support normal and streaming replies; non-text APIs return a JSON message. No upstream request or usage log is created.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </Dialog>
  )
}
