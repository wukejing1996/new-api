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
import { useEffect, useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check, Mail, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { searchUsers, sendEmailNotification } from '../api'
import type {
  EmailBroadcastRequest,
  EmailBroadcastResult,
  EmailBroadcastTargetType,
  User,
} from '../types'
import { useUsers } from './users-provider'

const SEARCH_PAGE_SIZE = 20

function mergeUsers(users: User[]) {
  const map = new Map<number, User>()
  for (const user of users) {
    map.set(user.id, user)
  }
  return [...map.values()]
}

function buildDisplayName(user: User) {
  const displayName = user.display_name || user.username
  return user.email ? `${displayName} <${user.email}>` : displayName
}

export function UsersEmailNotificationDialog() {
  const { t } = useTranslation()
  const { open, setOpen, selectedEmailUsers, setSelectedEmailUsers } =
    useUsers()
  const [targetType, setTargetType] =
    useState<EmailBroadcastTargetType>('all')
  const [subject, setSubject] = useState('')
  const [content, setContent] = useState('')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchResults, setSearchResults] = useState<User[]>([])
  const [searching, setSearching] = useState(false)
  const [lastResult, setLastResult] = useState<EmailBroadcastResult | null>(
    null
  )

  const isOpen = open === 'email'

  useEffect(() => {
    if (!isOpen) return
    setTargetType(selectedEmailUsers.length > 0 ? 'selected' : 'all')
    setLastResult(null)
  }, [isOpen, selectedEmailUsers.length])

  const selectedUserIds = useMemo(
    () => selectedEmailUsers.map((user) => user.id),
    [selectedEmailUsers]
  )

  const buildRequest = (dryRun: boolean): EmailBroadcastRequest => ({
    target: {
      type: targetType,
      user_ids: targetType === 'selected' ? selectedUserIds : undefined,
    },
    subject: subject.trim(),
    content: content.trim(),
    dry_run: dryRun,
  })

  const validateForm = (dryRun: boolean) => {
    if (!dryRun && !subject.trim()) {
      toast.error(t('Please enter an email subject'))
      return false
    }
    if (!dryRun && !content.trim()) {
      toast.error(t('Please enter email content'))
      return false
    }
    if (targetType === 'selected' && selectedUserIds.length === 0) {
      toast.error(t('Please select at least one user'))
      return false
    }
    return true
  }

  const previewMutation = useMutation({
    mutationFn: () => sendEmailNotification(buildRequest(true)),
    onSuccess: (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to preview recipients'))
        return
      }
      setLastResult(response.data)
      toast.success(
        t('{{count}} recipients matched.', {
          count: response.data.total,
        })
      )
    },
    onError: () => toast.error(t('Failed to preview recipients')),
  })

  const sendMutation = useMutation({
    mutationFn: () => sendEmailNotification(buildRequest(false)),
    onSuccess: (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to send email notification'))
        return
      }
      setLastResult(response.data)
      toast.success(
        t('Email notification sent to {{count}} users.', {
          count: response.data.sent,
        })
      )
    },
    onError: () => toast.error(t('Failed to send email notification')),
  })

  const handlePreview = () => {
    if (!validateForm(true)) return
    previewMutation.mutate()
  }

  const handleSend = () => {
    if (!validateForm(false)) return
    sendMutation.mutate()
  }

  const handleSearch = async () => {
    const keyword = searchKeyword.trim()
    if (!keyword) {
      setSearchResults([])
      return
    }

    setSearching(true)
    try {
      const response = await searchUsers({
        keyword,
        p: 1,
        page_size: SEARCH_PAGE_SIZE,
      })
      if (!response.success) {
        toast.error(response.message || t('Failed to search users'))
        return
      }
      setSearchResults(response.data?.items || [])
    } catch {
      toast.error(t('Failed to search users'))
    } finally {
      setSearching(false)
    }
  }

  const addUser = (user: User) => {
    setSelectedEmailUsers((prev) => mergeUsers([...prev, user]))
  }

  const removeUser = (userId: number) => {
    setSelectedEmailUsers((prev) => prev.filter((user) => user.id !== userId))
  }

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      setOpen(null)
      setSearchKeyword('')
      setSearchResults([])
      setLastResult(null)
    }
  }

  const isSending = sendMutation.isPending
  const isPreviewing = previewMutation.isPending
  const isBusy = isSending || isPreviewing

  return (
    <Dialog open={isOpen} onOpenChange={handleClose}>
      <DialogContent className='max-h-[90vh] overflow-hidden sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Mail className='h-5 w-5' />
            {t('Send Email Notification')}
          </DialogTitle>
          <DialogDescription>
            {t('Send an email notification to all users or selected users.')}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='max-h-[65vh] pr-3'>
          <div className='space-y-5'>
            <div className='space-y-2'>
              <Label>{t('Recipients')}</Label>
              <RadioGroup
                value={targetType}
                onValueChange={(value) => {
                  if (value !== null) {
                    setTargetType(value as EmailBroadcastTargetType)
                  }
                }}
                className='grid gap-2 sm:grid-cols-2'
              >
                <label className='border-input flex cursor-pointer items-start gap-3 rounded-lg border p-3'>
                  <RadioGroupItem value='all' />
                  <span className='space-y-1'>
                    <span className='block text-sm font-medium'>
                      {t('All users')}
                    </span>
                    <span className='text-muted-foreground block text-xs'>
                      {t('All enabled users with email addresses')}
                    </span>
                  </span>
                </label>
                <label className='border-input flex cursor-pointer items-start gap-3 rounded-lg border p-3'>
                  <RadioGroupItem value='selected' />
                  <span className='space-y-1'>
                    <span className='block text-sm font-medium'>
                      {t('Selected users')}
                    </span>
                    <span className='text-muted-foreground block text-xs'>
                      {t('Choose one or more users')}
                    </span>
                  </span>
                </label>
              </RadioGroup>
            </div>

            {targetType === 'selected' && (
              <div className='space-y-3'>
                <div className='flex gap-2'>
                  <Input
                    value={searchKeyword}
                    onChange={(event) => setSearchKeyword(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') {
                        event.preventDefault()
                        handleSearch()
                      }
                    }}
                    placeholder={t('Search username, name or email')}
                    disabled={isBusy}
                  />
                  <Button
                    type='button'
                    variant='outline'
                    onClick={handleSearch}
                    disabled={isBusy || searching}
                  >
                    <Search className='h-4 w-4' />
                    {searching ? t('Searching...') : t('Search')}
                  </Button>
                </div>

                {searchResults.length > 0 && (
                  <div className='border-input max-h-40 overflow-auto rounded-lg border'>
                    {searchResults.map((user) => {
                      const selected = selectedUserIds.includes(user.id)
                      return (
                        <button
                          key={user.id}
                          type='button'
                          className='hover:bg-muted flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm'
                          onClick={() => addUser(user)}
                          disabled={selected}
                        >
                          <span className='min-w-0 truncate'>
                            {buildDisplayName(user)}
                          </span>
                          {selected ? (
                            <Check className='text-primary h-4 w-4 shrink-0' />
                          ) : (
                            <span className='text-muted-foreground shrink-0 text-xs'>
                              {t('Add')}
                            </span>
                          )}
                        </button>
                      )
                    })}
                  </div>
                )}

                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('{{count}} selected users', {
                      count: selectedEmailUsers.length,
                    })}
                  </div>
                  {selectedEmailUsers.length > 0 ? (
                    <div className='flex max-h-28 flex-wrap gap-2 overflow-auto'>
                      {selectedEmailUsers.map((user) => (
                        <Badge
                          key={user.id}
                          variant='secondary'
                          className='gap-1 rounded-lg'
                        >
                          <span className='max-w-48 truncate'>
                            {buildDisplayName(user)}
                          </span>
                          <button
                            type='button'
                            onClick={() => removeUser(user.id)}
                            className='hover:text-destructive'
                            aria-label={t('Remove user')}
                          >
                            <X className='h-3 w-3' />
                          </button>
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <p className='text-muted-foreground text-sm'>
                      {t('No users selected')}
                    </p>
                  )}
                </div>
              </div>
            )}

            <div className='space-y-2'>
              <Label htmlFor='email-notification-subject'>
                {t('Email subject')}
              </Label>
              <Input
                id='email-notification-subject'
                value={subject}
                onChange={(event) => setSubject(event.target.value)}
                maxLength={120}
                disabled={isBusy}
              />
            </div>

            <div className='space-y-2'>
              <Label htmlFor='email-notification-content'>
                {t('Email content')}
              </Label>
              <Textarea
                id='email-notification-content'
                value={content}
                onChange={(event) => setContent(event.target.value)}
                rows={8}
                maxLength={10000}
                disabled={isBusy}
                placeholder={t('HTML content is supported')}
              />
            </div>

            {lastResult && (
              <div className='bg-muted/40 grid gap-2 rounded-lg border p-3 text-sm sm:grid-cols-4'>
                <div>
                  <div className='text-muted-foreground'>
                    {t('Matched recipients')}
                  </div>
                  <div className='font-medium'>{lastResult.total}</div>
                </div>
                <div>
                  <div className='text-muted-foreground'>{t('Sent')}</div>
                  <div className='font-medium'>{lastResult.sent}</div>
                </div>
                <div>
                  <div className='text-muted-foreground'>
                    {t('Skipped recipients')}
                  </div>
                  <div className='font-medium'>{lastResult.skipped}</div>
                </div>
                <div>
                  <div className='text-muted-foreground'>{t('Failed')}</div>
                  <div className='font-medium'>{lastResult.failed}</div>
                </div>
                {lastResult.failures && lastResult.failures.length > 0 && (
                  <div className='sm:col-span-4'>
                    <div className='text-muted-foreground mb-1'>
                      {t('Failure details')}
                    </div>
                    <div className='max-h-24 overflow-auto rounded-md border bg-background'>
                      {lastResult.failures.map((failure) => (
                        <div
                          key={`${failure.user_id}-${failure.email}`}
                          className='border-b px-2 py-1 text-xs last:border-b-0'
                        >
                          #{failure.user_id} {failure.email}: {failure.error}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button variant='outline' onClick={handlePreview} disabled={isBusy}>
            {isPreviewing ? t('Previewing...') : t('Preview recipients')}
          </Button>
          <Button onClick={handleSend} disabled={isBusy}>
            {isSending ? t('Sending...') : t('Send Email Notification')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
