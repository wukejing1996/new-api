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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { testRegistrationRiskQuery } from '../api'

export function RiskQueryTest(props: { configured: boolean }) {
  const { t } = useTranslation()
  const [ip, setIP] = useState('')
  const [email, setEmail] = useState('')
  const test = useMutation({
    mutationFn: () => testRegistrationRiskQuery(ip.trim(), email.trim()),
  })
  return (
    <SettingsSection title={t('Test risk assessment')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Uses saved settings. A test consumes API queries but does not create associations or change user groups.'
        )}
      </p>
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-2'>
          <Label htmlFor='risk-test-ip'>{t('IP address')}</Label>
          <Input
            id='risk-test-ip'
            value={ip}
            onChange={(event) => setIP(event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='risk-test-email'>{t('Email')}</Label>
          <Input
            id='risk-test-email'
            type='email'
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </div>
      </div>
      <Button
        type='button'
        variant='outline'
        className='w-fit'
        disabled={!props.configured || !ip.trim() || test.isPending}
        onClick={() => test.mutate()}
      >
        {test.isPending ? t('Testing...') : t('Run risk test')}
      </Button>
      {test.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {getServerErrorMessage(test.error, t('Risk test failed'))}
        </p>
      )}
      {test.data && !test.isError && (
        <div role='status' className='min-w-0 space-y-3 rounded-xl border p-4'>
          <p className='font-medium'>
            {test.data.restricted
              ? t('Restricted')
              : t('No enabled risk rule matched')}
          </p>
          {test.data.incomplete && <p>{t('Risk assessment incomplete')}</p>}
          <pre className='max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs'>
            {JSON.stringify(test.data, null, 2)}
          </pre>
        </div>
      )}
    </SettingsSection>
  )
}
