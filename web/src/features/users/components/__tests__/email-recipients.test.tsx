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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

import { USER_STATUS } from '../../constants'
import type { User } from '../../types'
import { UsersEmailNotificationDialog } from '../users-email-notification-dialog'
import { UsersProvider, useUsers } from '../users-provider'

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: vi.fn(() => []),
  })
})

afterEach(() => {
  Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

const disabledUser: User = {
  id: 2,
  username: 'disabled-user',
  display_name: 'Disabled user',
  status: USER_STATUS.DISABLED,
  email: 'disabled@example.com',
  role: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
}
const enabledUser: User = {
  ...disabledUser,
  id: 1,
  username: 'enabled-user',
  display_name: 'Enabled user',
  status: USER_STATUS.ENABLED,
  email: 'enabled@example.com',
}
const noEmailUser: User = {
  ...disabledUser,
  id: 3,
  username: 'no-email-user',
  display_name: 'No email user',
  email: '   ',
}

function OpenEmailDialog(props: { selected: User[] }) {
  const { setOpen, setSelectedEmailUsers } = useUsers()
  return (
    <Button
      onClick={() => {
        setSelectedEmailUsers(props.selected)
        setOpen('email')
      }}
    >
      Open email dialog
    </Button>
  )
}

function renderEmailDialog(selected: User[] = []) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [enabledUser, disabledUser, noEmailUser], total: 3 },
    },
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { total: 1, sent: 0, skipped: 0, failed: 0 } },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <OpenEmailDialog selected={selected} />
        <UsersEmailNotificationDialog />
      </UsersProvider>
    </QueryClientProvider>
  )
  return post
}

it('keeps a disabled user selected when opening from the user table and submits their ID', async () => {
  const post = renderEmailDialog([disabledUser])
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Open email dialog' }))
  expect(screen.getByRole('radio', { name: /Selected users/ })).toBeChecked()
  const checkbox = await screen.findByRole('checkbox', {
    name: /^Disabled user/,
  })
  expect(checkbox).not.toHaveAttribute('aria-disabled', 'true')
  expect(checkbox).toBeChecked()
  await user.type(screen.getByLabelText('Email subject'), 'Account notice')
  await user.type(screen.getByLabelText('Email content'), 'Account disabled')
  await user.click(screen.getByRole('button', { name: 'Send Email Notification' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/broadcast_email', {
      target: { type: 'selected', user_ids: [2] },
      subject: 'Account notice',
      content: 'Account disabled',
      dry_run: false,
    })
  )
})

it('selects disabled recipients individually and with select all while excluding users without an email', async () => {
  const post = renderEmailDialog()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Open email dialog' }))
  await user.click(screen.getByRole('radio', { name: /Selected users/ }))
  const disabled = await screen.findByRole('checkbox', {
    name: /^Disabled user/,
  })
  expect(disabled).not.toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByRole('checkbox', { name: /No email user/ })).toHaveAttribute(
    'aria-disabled',
    'true'
  )
  await user.click(disabled)
  await user.click(screen.getByRole('button', { name: 'Preview recipients' }))
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/user/broadcast_email',
      expect.objectContaining({
        target: { type: 'selected', user_ids: [2] },
        dry_run: true,
      })
    )
  )
  await user.click(screen.getByRole('checkbox', { name: 'Select all' }))
  expect(disabled).toBeChecked()
  expect(screen.getByRole('checkbox', { name: /Enabled user/ })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: /No email user/ })).not.toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Preview recipients' }))
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/user/broadcast_email',
      expect.objectContaining({ target: { type: 'selected', user_ids: [2, 1] } })
    )
  )
})
