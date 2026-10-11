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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  Ticket,
  TicketCounts,
  TicketListParams,
  TicketPage,
  TicketStatus,
} from './types'

type Response<T> = { success: boolean; message?: string; data: T }

export async function getTickets(
  params: TicketListParams,
  signal?: AbortSignal
): Promise<TicketPage> {
  const response = await api.get<Response<TicketPage>>('/api/ticket/admin', {
    signal,
    disableDuplicate: true,
    params: {
      p: params.page,
      page_size: params.page_size,
      keyword: params.keyword || undefined,
      unread: params.filter === 'unread' ? true : undefined,
      unreplied: params.filter === 'unreplied' ? true : undefined,
    },
  })
  return requireServerSuccess(response.data).data
}

export async function getTicketCounts(
  signal?: AbortSignal
): Promise<TicketCounts> {
  const response = await api.get<Response<TicketCounts>>(
    '/api/ticket/admin/counts',
    { signal, disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}

export async function getTicket(
  id: number,
  signal?: AbortSignal
): Promise<Ticket> {
  const response = await api.get<Response<Ticket>>(`/api/ticket/admin/${id}`, {
    signal,
    disableDuplicate: true,
  })
  return requireServerSuccess(response.data).data
}

export async function replyToTicket(
  id: number,
  content: string,
  images: File[]
): Promise<Ticket> {
  const form = new FormData()
  form.append('content', content)
  images.forEach((image) => form.append('images', image, image.name))
  const response = await api.post<Response<Ticket>>(
    `/api/ticket/admin/${id}/messages`,
    form
  )
  return requireServerSuccess(response.data).data
}

export async function setTicketStatus(
  id: number,
  status: TicketStatus
): Promise<Ticket> {
  const response = await api.put<Response<Ticket>>(
    `/api/ticket/admin/${id}/status`,
    { status }
  )
  return requireServerSuccess(response.data).data
}

export async function getTicketImage(
  id: number,
  signal?: AbortSignal
): Promise<Blob> {
  const response = await api.get<Blob>(`/api/ticket/images/${id}`, {
    responseType: 'blob',
    signal,
    disableDuplicate: true,
  })
  // Some API failures are JSON with a 200 status, not image data.
  if (response.data.type.includes('json')) {
    requireServerSuccess(JSON.parse(await response.data.text()))
  }
  if (!/^image\/(png|jpeg|gif|webp)$/.test(response.data.type)) {
    throw new Error('Invalid ticket attachment')
  }
  return response.data
}
