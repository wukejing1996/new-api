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
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { TimestampCell } from '@/components/activity-time-cell'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { getTicket, replyToTicket, setTicketStatus } from '../api'
import {
  ticketCategoryLabels,
  ticketPriorityLabels,
  ticketStatusLabels,
  type Ticket,
  type TicketStatus,
} from '../types'
import { TicketImage } from './ticket-image'

export function TicketDialog(props: { id: number; onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [files, setFiles] = useState<File[]>([])
  const [fileError, setFileError] = useState('')
  const conversation = useRef<HTMLDivElement>(null)
  const schema = z.object({
    content: z
      .string()
      .trim()
      .min(1, t('Enter a reply.'))
      .max(20000, t('Reply must be at most 20,000 characters.')),
  })
  const form = useForm<{ content: string }>({
    resolver: zodResolver(schema),
    defaultValues: { content: '' },
  })
  const detailKey = ['admin-tickets', 'detail', props.id]
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getTicket(props.id, signal),
    retry: false,
  })
  // Reading a ticket marks it read on the server; refresh the queue and counters.
  useEffect(() => {
    if (!detail.dataUpdatedAt) return
    void queryClient.invalidateQueries({ queryKey: ['admin-tickets', 'list'] })
    void queryClient.invalidateQueries({
      queryKey: ['admin-tickets', 'counts'],
    })
  }, [detail.dataUpdatedAt, queryClient])
  useEffect(() => {
    if (conversation.current) {
      conversation.current.scrollTop = conversation.current.scrollHeight
    }
  }, [detail.data?.messages?.length])

  function updateTicket(ticket: Ticket): void {
    queryClient.setQueryData(detailKey, ticket)
    void queryClient.invalidateQueries({ queryKey: ['admin-tickets', 'list'] })
    void queryClient.invalidateQueries({
      queryKey: ['admin-tickets', 'counts'],
    })
  }
  const reply = useMutation({
    mutationFn: (values: { content: string }) =>
      replyToTicket(props.id, values.content, files),
    onSuccess: (ticket) => {
      updateTicket(ticket)
      form.reset()
      setFiles([])
      setFileError('')
    },
    onError: (error) => handleServerError(error),
  })
  const status = useMutation({
    mutationFn: (value: TicketStatus) => setTicketStatus(props.id, value),
    // Status responses may omit messages; retain the loaded conversation.
    onSuccess: (ticket) => updateTicket({ ...detail.data, ...ticket }),
  })
  const busy = reply.isPending || status.isPending
  const ticket = detail.data

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) props.onClose()
      }}
      title={ticket?.subject || t('Support tickets')}
      description={
        ticket
          ? `${t('Submitted by')}: ${ticket.user_name || `#${ticket.user_id}`} · #${ticket.id}`
          : undefined
      }
      contentClassName='sm:max-w-4xl'
      bodyClassName='space-y-4'
      showCloseButton={!busy}
    >
      {detail.isLoading && <LoadingState />}
      {detail.isError && (
        <ErrorState
          description={t('Tickets could not be loaded.')}
          onRetry={() => void detail.refetch()}
        />
      )}
      {ticket && (
        <>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <p className='text-muted-foreground text-sm'>
              {t(ticketCategoryLabels[ticket.category] || ticket.category)} ·{' '}
              {t(ticketPriorityLabels[ticket.priority] || ticket.priority)}
            </p>
            <NativeSelect
              aria-label={t('Status')}
              value={ticket.status}
              disabled={busy}
              onChange={(event) =>
                status.mutate(event.target.value as TicketStatus)
              }
            >
              {Object.entries(ticketStatusLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {t(label)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div
            ref={conversation}
            role='log'
            aria-label={t('Conversation')}
            className='max-h-[42dvh] space-y-3 overflow-y-auto rounded-lg border p-3'
          >
            {(ticket.messages ?? []).map((message) => (
              <article
                key={message.id}
                className={`max-w-[95%] rounded-lg border p-3 ${message.sender_type === 'admin' ? 'bg-muted ml-auto' : 'bg-background'}`}
              >
                <div className='text-muted-foreground mb-2 flex flex-wrap justify-between gap-2 text-xs'>
                  <strong>
                    {message.sender_type === 'admin'
                      ? t('Administrator')
                      : ticket.user_name || `#${ticket.user_id}`}
                  </strong>
                  <TimestampCell
                    timestamp={message.created_at}
                    justNowLabel={t('Just now')}
                    format='absolute'
                  />
                </div>
                <p className='text-sm break-words whitespace-pre-wrap'>
                  {message.content}
                </p>
                <div className='mt-2 flex flex-wrap gap-2'>
                  {message.images?.map((image) => (
                    <TicketImage key={image.id} image={image} />
                  ))}
                </div>
              </article>
            ))}
          </div>
          {ticket.status !== 'closed' && (
            <form
              className='space-y-3'
              onSubmit={form.handleSubmit((values) => {
                if (!busy) reply.mutate(values)
              })}
            >
              <Field data-invalid={Boolean(form.formState.errors.content)}>
                <FieldLabel htmlFor={`ticket-reply-${props.id}`}>
                  {t('Reply')}
                </FieldLabel>
                <Textarea
                  id={`ticket-reply-${props.id}`}
                  {...form.register('content')}
                  rows={4}
                  maxLength={20000}
                  disabled={busy}
                  aria-invalid={Boolean(form.formState.errors.content)}
                  aria-describedby={
                    form.formState.errors.content
                      ? 'ticket-reply-error'
                      : undefined
                  }
                />
                {form.formState.errors.content && (
                  <p
                    id='ticket-reply-error'
                    role='alert'
                    className='text-destructive text-sm'
                  >
                    {form.formState.errors.content.message}
                  </p>
                )}
              </Field>
              <Field>
                <FieldLabel htmlFor={`ticket-files-${props.id}`}>
                  {t('Attachments')}
                </FieldLabel>
                <p className='text-muted-foreground text-xs'>
                  {t('PNG, JPEG, GIF, or WebP. Up to 3 files, 2 MB each.')}
                </p>
                <Input
                  id={`ticket-files-${props.id}`}
                  type='file'
                  multiple
                  accept='image/png,image/jpeg,image/gif,image/webp'
                  disabled={busy || files.length >= 3}
                  onChange={(event) => {
                    const added = [...(event.target.files ?? [])].filter(
                      (file) =>
                        !files.some(
                          (existing) =>
                            existing.name === file.name &&
                            existing.size === file.size &&
                            existing.lastModified === file.lastModified
                        )
                    )
                    event.target.value = ''
                    if (
                      files.length + added.length > 3 ||
                      added.some(
                        (file) =>
                          file.size > 2 * 1024 * 1024 ||
                          !/^image\/(png|jpeg|gif|webp)$/.test(file.type)
                      )
                    ) {
                      setFileError(
                        t('PNG, JPEG, GIF, or WebP. Up to 3 files, 2 MB each.')
                      )
                      return
                    }
                    setFiles((previous) => [...previous, ...added])
                    setFileError('')
                  }}
                />
                {fileError && (
                  <p role='alert' className='text-destructive text-sm'>
                    {fileError}
                  </p>
                )}
                {files.map((file, index) => (
                  <div
                    key={`${file.name}-${file.size}-${file.lastModified}`}
                    className='flex min-w-0 items-center justify-between gap-2 text-sm'
                  >
                    <span className='truncate'>{file.name}</span>
                    <Button
                      type='button'
                      variant='ghost'
                      disabled={busy}
                      onClick={() => {
                        setFiles((previous) =>
                          previous.filter((_, position) => position !== index)
                        )
                        setFileError('')
                      }}
                    >
                      {t('Remove attachment')}
                    </Button>
                  </div>
                ))}
              </Field>
              <div className='flex justify-end'>
                <Button type='submit' variant='secondary' disabled={busy}>
                  {reply.isPending ? t('Sending...') : t('Send reply')}
                </Button>
              </div>
            </form>
          )}
        </>
      )}
    </Dialog>
  )
}
