import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getRegistrationPartnerMappings,
  saveRegistrationPartnerMappings,
  type RegistrationPartnerMapping,
} from '../api'

const queryKey = ['registration-partner-mappings']
type MappingRow = RegistrationPartnerMapping & { id: number }

export function RegistrationPartners({ groups }: { groups?: string[] }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const fieldPrefix = useId()
  const { copyToClipboard } = useCopyToClipboard()
  const verification = useSecureVerification()
  const [draft, setDraft] = useState<MappingRow[] | null>(null)
  const [nextId, setNextId] = useState(100)
  const mappings = useQuery({
    queryKey,
    queryFn: getRegistrationPartnerMappings,
    gcTime: 0,
  })
  const save = useMutation({
    mutationFn: ({
      mappings,
      proofToken,
    }: {
      mappings: RegistrationPartnerMapping[]
      proofToken: string
    }) => saveRegistrationPartnerMappings(mappings, proofToken),
    onSuccess: (data) => {
      client.setQueryData(queryKey, data)
      setDraft(null)
      toast.success(t('Settings saved'))
    },
  })
  const availableGroups = groups
    ?.filter((group) => group !== 'auto' && group !== 'all')
    .sort()
  const rows =
    draft ??
    (mappings.data ?? []).map(({ code, group }, id) => ({ code, group, id }))
  const valid =
    rows.every(
      ({ code, group }) =>
        /^[A-Za-z0-9_-]{16,128}$/.test(code) && availableGroups?.includes(group)
    ) && new Set(rows.map(({ code }) => code)).size === rows.length
  const saving = save.isPending || verification.isActive

  function updateRow(id: number, update: Partial<RegistrationPartnerMapping>) {
    save.reset()
    setDraft(rows.map((row) => (row.id === id ? { ...row, ...update } : row)))
  }

  async function submitMappings() {
    if (!draft || !valid || !availableGroups || saving) return
    const proof = await verification.requestVerification({
      scope: 'registration.partners.update',
    })
    if (proof) {
      save.mutate({
        mappings: rows.map(({ code, group }) => ({ code, group })),
        proofToken: proof.proof_token,
      })
    }
  }

  return (
    <SettingsSection title={t('Partner registration links')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Anyone with the link can reuse it to join the selected group and skip registration risk checks. Share it privately.'
        )}
      </p>
      {mappings.isPending && <LoadingState />}
      {mappings.isError && (
        <ErrorState
          title={t('Failed to load settings')}
          onRetry={() => void mappings.refetch()}
        />
      )}
      {mappings.isSuccess && (
        <form
          aria-label={t('Partner registration links')}
          className='flex flex-col gap-4'
          onSubmit={(event) => {
            event.preventDefault()
            void submitMappings()
          }}
        >
          <p className='text-muted-foreground text-sm'>
            {t(
              'Partner codes must use 16-128 letters, digits, hyphens or underscores'
            )}
            . {t('Partner registration codes must be unique')}.
          </p>
          {rows.map(({ id, code, group }) => {
            const saved = mappings.data?.find(
              (entry) => entry.code === code && entry.group === group
            )
            const groupMissing =
              group && availableGroups && !availableGroups.includes(group)
            return (
              <div
                key={id}
                className='grid gap-3 rounded-lg border p-3 sm:grid-cols-2'
              >
                <label
                  htmlFor={`${fieldPrefix}-group-${id}`}
                  className='grid min-w-0 gap-1 text-sm font-medium'
                >
                  {t('User group')}
                  <NativeSelect
                    id={`${fieldPrefix}-group-${id}`}
                    value={group}
                    className='w-full'
                    disabled={saving || !availableGroups}
                    onChange={(event) =>
                      updateRow(id, { group: event.target.value })
                    }
                  >
                    <NativeSelectOption value=''>
                      {t('Select an existing user group')}
                    </NativeSelectOption>
                    {group && (!availableGroups || groupMissing) && (
                      <NativeSelectOption value={group}>
                        {group}
                      </NativeSelectOption>
                    )}
                    {availableGroups?.map((name) => (
                      <NativeSelectOption key={name} value={name}>
                        {name}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  {groupMissing && (
                    <span className='text-destructive'>
                      {t('Select an existing user group')}
                    </span>
                  )}
                </label>
                <label
                  htmlFor={`${fieldPrefix}-code-${id}`}
                  className='grid min-w-0 gap-1 text-sm font-medium'
                >
                  {t('Partner code')}
                  <Input
                    id={`${fieldPrefix}-code-${id}`}
                    type='password'
                    autoComplete='off'
                    value={code}
                    maxLength={128}
                    required
                    pattern={String.raw`[A-Za-z0-9_\-]{16,128}`}
                    disabled={saving}
                    onChange={(event) =>
                      updateRow(id, { code: event.target.value })
                    }
                  />
                </label>
                {saved?.link && !groupMissing && (
                  <div className='flex flex-wrap items-end gap-2 sm:col-span-2'>
                    <label
                      htmlFor={`${fieldPrefix}-link-${id}`}
                      className='grid min-w-0 flex-1 basis-48 gap-1 text-sm font-medium'
                    >
                      {t('Registration URL')}
                      <Input
                        id={`${fieldPrefix}-link-${id}`}
                        value={saved.link}
                        readOnly
                        onFocus={(event) => event.currentTarget.select()}
                      />
                    </label>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() => void copyToClipboard(saved.link)}
                    >
                      {t('Copy Link')}
                    </Button>
                  </div>
                )}
                {saved && !saved.link && !groupMissing && (
                  <p className='text-muted-foreground text-sm sm:col-span-2'>
                    {t(
                      'Set the public HTTPS site address to display the registration link.'
                    )}
                  </p>
                )}
                <Button
                  type='button'
                  variant='outline'
                  className='w-fit'
                  disabled={saving}
                  onClick={() => {
                    save.reset()
                    setDraft(rows.filter((row) => row.id !== id))
                  }}
                >
                  {t('Remove')}
                </Button>
              </div>
            )
          })}
          {save.isError && (
            <p role='alert' className='text-destructive text-sm'>
              {getServerErrorMessage(
                save.error,
                t('Failed to save risk settings')
              )}
            </p>
          )}
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={
                saving || !availableGroups?.length || rows.length >= 100
              }
              onClick={() => {
                save.reset()
                setDraft([...rows, { id: nextId, code: '', group: '' }])
                setNextId(nextId + 1)
              }}
            >
              {t('Add')}
            </Button>
            <Button
              type='submit'
              variant='secondary'
              disabled={saving || !draft || !valid || !availableGroups}
            >
              {save.isPending ? t('Saving...') : t('Save changes')}
            </Button>
          </div>
        </form>
      )}
      <SecureVerificationDialog {...verification.dialogProps} />
    </SettingsSection>
  )
}
