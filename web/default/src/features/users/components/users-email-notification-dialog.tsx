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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Eye, Mail, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { getUsers, searchUsers, sendEmailNotification } from '../api'
import { USER_STATUS } from '../constants'
import type {
  EmailBroadcastRequest,
  EmailBroadcastResult,
  EmailBroadcastTargetType,
  User,
} from '../types'
import { useUsers } from './users-provider'

const USER_LIST_PAGE_SIZE = 8

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

function canEmailUser(user: User) {
  return user.status === USER_STATUS.ENABLED && !!user.email?.trim()
}

function buildPreviewSrcDoc(content: string) {
  const html = content.trim()
  if (!html) return ''
  if (/^<!doctype/i.test(html) || /<html[\s>]/i.test(html)) return html
  return `<!doctype html><html><head><base target="_blank"><style>body{margin:0;padding:24px;font-family:Arial,sans-serif;color:#111827;background:#fff;line-height:1.5}img{max-width:100%;height:auto}</style></head><body>${html}</body></html>`
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
  const [searchQuery, setSearchQuery] = useState('')
  const [userPage, setUserPage] = useState(1)
  const [contentPreviewOpen, setContentPreviewOpen] = useState(false)
  const [lastResult, setLastResult] = useState<EmailBroadcastResult | null>(
    null
  )
  const wasOpenRef = useRef(false)

  const isOpen = open === 'email'

  useEffect(() => {
    if (!isOpen) {
      wasOpenRef.current = false
      return
    }
    if (wasOpenRef.current) return
    wasOpenRef.current = true

    const emailUsers = selectedEmailUsers.filter(canEmailUser)
    if (emailUsers.length !== selectedEmailUsers.length) {
      setSelectedEmailUsers(emailUsers)
    }
    setTargetType(emailUsers.length > 0 ? 'selected' : 'all')
    setLastResult(null)
  }, [isOpen, selectedEmailUsers, setSelectedEmailUsers])

  const selectedUserIds = useMemo(
    () => selectedEmailUsers.map((user) => user.id),
    [selectedEmailUsers]
  )
  const selectedUserIdSet = useMemo(
    () => new Set(selectedUserIds),
    [selectedUserIds]
  )

  const usersQuery = useQuery({
    queryKey: ['email-recipient-users', userPage, searchQuery],
    queryFn: async () => {
      const params = {
        p: userPage,
        page_size: USER_LIST_PAGE_SIZE,
      }
      const response = searchQuery
        ? await searchUsers({ ...params, keyword: searchQuery })
        : await getUsers(params)

      if (!response.success) {
        toast.error(response.message || t('Failed to load users'))
        return { items: [], total: 0 }
      }

      return {
        items: response.data?.items || [],
        total: response.data?.total || 0,
      }
    },
    enabled: isOpen && targetType === 'selected',
    placeholderData: (previousData) => previousData,
  })

  const users = usersQuery.data?.items || []
  const totalUsers = usersQuery.data?.total || 0
  const userPageCount = Math.max(1, Math.ceil(totalUsers / USER_LIST_PAGE_SIZE))
  const selectableUsers = users.filter(canEmailUser)
  const allPageUsersSelected =
    selectableUsers.length > 0 &&
    selectableUsers.every((user) => selectedUserIdSet.has(user.id))
  const somePageUsersSelected =
    !allPageUsersSelected &&
    selectableUsers.some((user) => selectedUserIdSet.has(user.id))
  const previewSrcDoc = buildPreviewSrcDoc(content)

  useEffect(() => {
    if (userPage > userPageCount) {
      setUserPage(userPageCount)
    }
  }, [userPage, userPageCount])

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

  const handleSearch = () => {
    setSearchQuery(searchKeyword.trim())
    setUserPage(1)
  }

  const toggleUser = (user: User, checked: boolean) => {
    if (!canEmailUser(user)) return
    setSelectedEmailUsers((prev) =>
      checked
        ? mergeUsers([...prev, user])
        : prev.filter((item) => item.id !== user.id)
    )
  }

  const removeUser = (userId: number) => {
    setSelectedEmailUsers((prev) => prev.filter((user) => user.id !== userId))
  }

  const togglePageUsers = (checked: boolean) => {
    setSelectedEmailUsers((prev) => {
      if (checked) {
        return mergeUsers([...prev, ...selectableUsers])
      }

      const pageUserIds = new Set(selectableUsers.map((user) => user.id))
      return prev.filter((user) => !pageUserIds.has(user.id))
    })
  }

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      setOpen(null)
      setSearchKeyword('')
      setSearchQuery('')
      setUserPage(1)
      setContentPreviewOpen(false)
      setLastResult(null)
    }
  }

  const isSending = sendMutation.isPending
  const isPreviewing = previewMutation.isPending
  const isLoadingUsers = usersQuery.isLoading || usersQuery.isFetching
  const isBusy = isSending || isPreviewing

  return (
    <>
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
                    disabled={isBusy || isLoadingUsers}
                  >
                    <Search className='h-4 w-4' />
                    {isLoadingUsers ? t('Searching...') : t('Search')}
                  </Button>
                </div>

                <div className='border-input overflow-hidden rounded-lg border'>
                  <div className='bg-muted/40 flex items-center gap-3 border-b px-3 py-2'>
                    <Checkbox
                      checked={allPageUsersSelected}
                      indeterminate={somePageUsersSelected}
                      onCheckedChange={(value) => togglePageUsers(!!value)}
                      disabled={isBusy || selectableUsers.length === 0}
                      aria-label={t('Select all')}
                    />
                    <div className='min-w-0 flex-1 text-sm font-medium'>
                      {t('Recipients')}
                    </div>
                    <div className='text-muted-foreground shrink-0 text-xs'>
                      {t('Page {{current}} of {{total}}', {
                        current: userPage,
                        total: userPageCount,
                      })}
                    </div>
                  </div>
                  <div className='max-h-64 overflow-auto'>
                    {isLoadingUsers ? (
                      <div className='text-muted-foreground px-3 py-8 text-center text-sm'>
                        {t('Loading...')}
                      </div>
                    ) : users.length > 0 ? (
                      users.map((user) => {
                        const canSelect = canEmailUser(user)
                        const selected = selectedUserIdSet.has(user.id)
                        return (
                          <label
                            key={user.id}
                            className='hover:bg-muted flex cursor-pointer items-center gap-3 border-b px-3 py-2 text-sm last:border-b-0 has-disabled:cursor-not-allowed has-disabled:opacity-60'
                          >
                            <Checkbox
                              checked={selected}
                              onCheckedChange={(value) =>
                                toggleUser(user, !!value)
                              }
                              disabled={isBusy || !canSelect}
                              aria-label={buildDisplayName(user)}
                            />
                            <span className='min-w-0 flex-1'>
                              <span className='block truncate font-medium'>
                                {user.display_name || user.username}
                              </span>
                              <span className='text-muted-foreground block truncate text-xs'>
                                {user.email || t('No email address')}
                              </span>
                            </span>
                            {!canSelect && (
                              <Badge variant='secondary' className='shrink-0'>
                                {user.email ? t('Disabled') : t('No email')}
                              </Badge>
                            )}
                          </label>
                        )
                      })
                    ) : (
                      <div className='text-muted-foreground px-3 py-8 text-center text-sm'>
                        {t('No Users Found')}
                      </div>
                    )}
                  </div>
                  <div className='flex items-center justify-between gap-2 border-t px-3 py-2'>
                    <div className='text-muted-foreground text-xs'>
                      {totalUsers} {t('Users')}
                    </div>
                    <div className='flex gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => setUserPage((page) => page - 1)}
                        disabled={isBusy || isLoadingUsers || userPage <= 1}
                        aria-label={t('Previous page')}
                      >
                        <ChevronLeft className='h-4 w-4' />
                      </Button>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => setUserPage((page) => page + 1)}
                        disabled={
                          isBusy ||
                          isLoadingUsers ||
                          userPage >= userPageCount
                        }
                        aria-label={t('Next page')}
                      >
                        <ChevronRight className='h-4 w-4' />
                      </Button>
                    </div>
                  </div>
                </div>

                <div className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <div className='text-sm font-medium'>
                      {t('{{count}} selected users', {
                        count: selectedEmailUsers.length,
                      })}
                    </div>
                    {selectedEmailUsers.length > 0 && (
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        onClick={() => setSelectedEmailUsers([])}
                        disabled={isBusy}
                      >
                        {t('Clear selection')}
                      </Button>
                    )}
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
              <div className='flex items-center justify-between gap-2'>
                <Label htmlFor='email-notification-content'>
                  {t('Email content')}
                </Label>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => setContentPreviewOpen(true)}
                  disabled={!content.trim()}
                >
                  <Eye className='h-4 w-4' />
                  {t('Preview email')}
                </Button>
              </div>
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

      <Dialog open={contentPreviewOpen} onOpenChange={setContentPreviewOpen}>
        <DialogContent className='max-h-[90vh] sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Email preview')}</DialogTitle>
            <DialogDescription>
              {t('Rendered HTML email content preview')}
            </DialogDescription>
          </DialogHeader>
          <div className='overflow-hidden rounded-lg border bg-background'>
            <iframe
              title={t('Email preview')}
              srcDoc={previewSrcDoc}
              sandbox=''
              className='h-[55vh] w-full bg-background'
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
