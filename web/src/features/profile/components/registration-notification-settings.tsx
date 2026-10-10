import { useMutation } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
} from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { sendRegistrationNotificationTest } from '../api'

interface RegistrationNotificationSettingsProps {
  enabled: boolean
  onEnabledChange: (enabled: boolean) => void
  disabled: boolean
  unsaved: boolean
}

export function RegistrationNotificationSettings(
  props: RegistrationNotificationSettingsProps
) {
  const { t } = useTranslation()
  const test = useMutation({
    mutationFn: sendRegistrationNotificationTest,
    onSuccess: () => toast.success(t('Test notification sent')),
    onError: (error) =>
      handleServerError(error, t('Failed to send test notification')),
  })

  return (
    <div className='space-y-3 rounded-lg border p-3 sm:p-4'>
      <Field orientation='horizontal' data-disabled={props.disabled}>
        <FieldContent>
          <FieldLabel htmlFor='newUserRegistrationNotify'>
            {t('Receive New User Registration Notifications')}
          </FieldLabel>
          <FieldDescription>
            {t(
              'Admins only. Receive new account details through your selected notification method after successful registration.'
            )}
          </FieldDescription>
        </FieldContent>
        <Switch
          id='newUserRegistrationNotify'
          checked={props.enabled}
          onCheckedChange={props.onEnabledChange}
          disabled={props.disabled}
        />
      </Field>
      <p className='text-muted-foreground text-xs'>
        {t('Save notification settings before sending a test.')}
      </p>
      <Button
        type='button'
        variant='outline'
        disabled={props.disabled || props.unsaved || test.isPending}
        onClick={() => test.mutate()}
      >
        {test.isPending && <Loader2 className='size-4 animate-spin' />}
        {t('Send Test Notification')}
      </Button>
    </div>
  )
}
