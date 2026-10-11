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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { Toaster, toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { Tickets } from '..'
import { TicketDialog } from '../components/ticket-dialog'
import type { Ticket } from '../types'

const clients: QueryClient[] = []
const ticket: Ticket = {
  id: 41,
  user_id: 7,
  user_name: 'Customer',
  subject: 'API connection issue',
  category: 'api',
  priority: 'high',
  status: 'open',
  updated_at: 1,
  unread: true,
  unreplied: true,
  messages: [
    {
      id: 1,
      sender_type: 'user',
      content: 'Please investigate this request.',
      created_at: 1,
    },
  ],
}

function mount(children: ReactNode): QueryClient {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      {children}
      <Toaster />
    </QueryClientProvider>
  )
  return client
}

afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  toast.dismiss()
})

test('filtering the admin queue uses admin endpoints and opening a ticket refreshes unread counts', async () => {
  const requests: { url: string; params?: Record<string, unknown> }[] = []
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    requests.push({ url, params: config?.params })
    if (url.endsWith('/counts')) {
      return {
        data: { success: true, data: { unread_count: 1, unreplied_count: 1 } },
      }
    }
    if (url === '/api/ticket/admin/41') {
      return { data: { success: true, data: ticket } }
    }
    return { data: { success: true, data: { items: [ticket], total: 1 } } }
  })
  mount(<Tickets />)
  await screen.findByRole('button', { name: ticket.subject })
  fireEvent.change(screen.getByLabelText('Ticket queue'), {
    target: { value: 'unread' },
  })
  await waitFor(() =>
    expect(requests.some((request) => request.params?.unread === true)).toBe(
      true
    )
  )
  await userEvent.click(screen.getByRole('button', { name: ticket.subject }))
  await screen.findByRole('log', { name: 'Conversation' })
  await waitFor(() =>
    expect(
      requests.filter((request) => request.url.endsWith('/counts')).length
    ).toBeGreaterThan(1)
  )
  expect(screen.getByLabelText('Status')).toHaveValue('open')
})

test('a closed ticket hides the composer and reopening preserves the conversation', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...ticket, status: 'closed' } },
  })
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, data: { id: 41, status: 'open' } },
  })
  mount(<TicketDialog id={41} onClose={() => undefined} />)
  await screen.findByRole('log')
  expect(
    screen.queryByRole('button', { name: 'Send reply' })
  ).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Status'), {
    target: { value: 'open' },
  })
  await screen.findByRole('button', { name: 'Send reply' })
  expect(api.put).toHaveBeenCalledWith('/api/ticket/admin/41/status', {
    status: 'open',
  })
  expect(screen.getByRole('log')).toHaveTextContent(
    'Please investigate this request.'
  )
})

test('empty replies are rejected; failed replies preserve the draft and attachments; success clears them', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: ticket },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: { success: false, message: 'Reply rejected' },
    })
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          ...ticket,
          messages: [
            ...(ticket.messages ?? []),
            {
              id: 2,
              sender_type: 'admin',
              content: 'We are investigating.',
              created_at: 2,
            },
          ],
        },
      },
    })
  mount(<TicketDialog id={41} onClose={() => undefined} />)
  await screen.findByRole('log')
  await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Enter a reply.')
  expect(post).not.toHaveBeenCalled()
  await userEvent.type(screen.getByLabelText('Reply'), 'We are investigating.')
  const file = new File(['image'], 'issue.png', { type: 'image/png' })
  fireEvent.change(screen.getByLabelText('Attachments'), {
    target: { files: [file] },
  })
  await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
  await screen.findByText('Reply rejected')
  expect(screen.getByLabelText('Reply')).toHaveValue('We are investigating.')
  expect(screen.getByText('issue.png')).toBeInTheDocument()
  const form = post.mock.calls[0][1] as FormData
  expect(form.get('content')).toBe('We are investigating.')
  expect((form.get('images') as File).name).toBe('issue.png')
  await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
  await waitFor(() => expect(screen.getByLabelText('Reply')).toHaveValue(''))
  expect(screen.queryByText('issue.png')).not.toBeInTheDocument()
  expect(screen.getByRole('log')).toHaveTextContent('We are investigating.')
})

test('invalid attachments are rejected without adding them to the reply', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: ticket },
  })
  mount(<TicketDialog id={41} onClose={() => undefined} />)
  await screen.findByRole('log')
  const file = new File(['<svg/>'], 'unsafe.svg', { type: 'image/svg+xml' })
  fireEvent.change(screen.getByLabelText('Attachments'), {
    target: { files: [file] },
  })
  expect(screen.getByRole('alert')).toHaveTextContent('Up to 3 files')
  expect(screen.queryByText('unsafe.svg')).not.toBeInTheDocument()
})

test('business failures on ticket loading show retry and never show a composer', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'Ticket not found' },
  })
  mount(<TicketDialog id={41} onClose={() => undefined} />)
  await screen.findByRole('button', { name: 'Retry' })
  expect(screen.queryByLabelText('Reply')).not.toBeInTheDocument()
})
