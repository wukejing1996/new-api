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
export type TicketStatus = 'open' | 'processing' | 'resolved' | 'closed'
export type TicketFilter = 'all' | 'unread' | 'unreplied'

export type TicketImage = {
  id: number
  file_name: string
  mime_type: string
  file_size: number
}

export type TicketMessage = {
  id: number
  sender_type: 'user' | 'admin'
  content: string
  created_at: number
  images?: TicketImage[]
}

export type Ticket = {
  id: number
  user_id: number
  user_name?: string
  subject: string
  category: string
  priority: string
  status: TicketStatus
  updated_at: number
  unread?: boolean
  unreplied?: boolean
  messages?: TicketMessage[]
}

export type TicketPage = { items: Ticket[]; total: number }
export type TicketCounts = { unread_count: number; unreplied_count: number }
export type TicketListParams = {
  page: number
  page_size: number
  keyword: string
  filter: TicketFilter
}

export const ticketStatusLabels: Record<TicketStatus, string> = {
  open: 'Open',
  processing: 'Processing',
  resolved: 'Resolved',
  closed: 'Closed',
}

export const ticketCategoryLabels: Record<string, string> = {
  account: 'Account',
  api: 'API',
  billing: 'Billing',
  other: 'Other',
}

export const ticketPriorityLabels: Record<string, string> = {
  normal: 'Normal',
  high: 'High',
  urgent: 'Urgent',
}
