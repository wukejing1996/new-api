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
import {
  fireEvent,
  render as rtlRender,
  screen,
  waitFor,
} from '@testing-library/react'
import type { ReactElement } from 'react'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'

import { sendRegistrationNotificationTest, updateUserSettings } from '../api'
import { NotificationTab } from '../components/tabs/notification-tab'
import type { UserProfile } from '../types'

const profile: UserProfile = {
  id: 1,
  username: 'alice',
  display_name: 'Alice',
  role: 1,
  group: 'default',
  quota: 1000000,
  used_quota: 0,
  request_count: 0,
  status: 1,
  aff_count: 0,
  aff_quota: 0,
  aff_history_quota: 0,
  created_time: 0,
}
const settings = {
  notify_type: 'webhook',
  quota_warning_threshold: 1200,
  notification_email: '',
  webhook_url: 'https://example.com/notify',
  webhook_secret: 'webhook-secret',
  bark_url: '',
  gotify_url: '',
  gotify_token: '',
  gotify_priority: 5,
  accept_unset_model_ratio_model: true,
  record_ip_log: true,
  upstream_model_update_notify_enabled: true,
  new_user_registration_notify_enabled: false,
}

const queryClients: QueryClient[] = []
function render(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  queryClients.push(client)
  return rtlRender(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>
  )
}
afterEach(() => {
  queryClients.forEach((client) => client.clear())
  queryClients.length = 0
  vi.restoreAllMocks()
})

/** The body a PUT sends over the wire: undefined fields are left out. */
function sentBody(put: { mock: { calls: unknown[][] } }): unknown {
  return JSON.parse(JSON.stringify(put.mock.calls[0][1]))
}

describe('user settings saves across profile and security', () => {
  it('disabling IP recording sends the latest complete notification settings', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { ...profile, setting: JSON.stringify(settings) },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await updateUserSettings({ record_ip_log: false })
    expect(get).toHaveBeenCalledWith('/api/user/self')
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      ...settings,
      record_ip_log: false,
    })
  })

  it('saving IP recording for a user with no settings supplies the defaults and leaves unchosen switches unset', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { ...profile, role: ROLE.SUPER_ADMIN } },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await updateUserSettings({ record_ip_log: true })
    expect(put.mock.calls[0][0]).toBe('/api/user/setting')
    expect(sentBody(put)).toEqual({
      notify_type: 'email',
      quota_warning_threshold: 0,
      notification_email: '',
      webhook_url: '',
      webhook_secret: '',
      bark_url: '',
      gotify_url: '',
      gotify_token: '',
      gotify_priority: 5,
      accept_unset_model_ratio_model: false,
      record_ip_log: true,
      upstream_model_update_notify_enabled: false,
      new_user_registration_notify_enabled: false,
    })
  })

  it('alternating notification and IP saves retains the latest values from each page', async () => {
    let saved = { ...settings }
    vi.spyOn(api, 'get').mockImplementation(async () => ({
      data: {
        success: true,
        data: { ...profile, setting: JSON.stringify(saved) },
      },
    }))
    vi.spyOn(api, 'put').mockImplementation(async (_url, body) => {
      saved = body as typeof settings
      return { data: { success: true } }
    })
    await updateUserSettings({ record_ip_log: false })
    await updateUserSettings({ quota_warning_threshold: 2500 })
    await updateUserSettings({ record_ip_log: true })
    expect(saved).toEqual({
      ...settings,
      quota_warning_threshold: 2500,
      record_ip_log: true,
    })
  })

  it('a rejected profile read prevents the settings write', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('offline'))
    const put = vi.spyOn(api, 'put')
    await expect(updateUserSettings({ record_ip_log: false })).rejects.toThrow(
      'offline'
    )
    expect(put).not.toHaveBeenCalled()
  })

  it('an unsuccessful profile response prevents the settings write', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: false, message: 'Unavailable' },
    })
    const put = vi.spyOn(api, 'put')
    expect(await updateUserSettings({ record_ip_log: false })).toEqual({
      success: false,
      message: 'Unavailable',
    })
    expect(put).not.toHaveBeenCalled()
  })

  it('an unsuccessful settings write is returned to the caller', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: profile },
    })
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'Save failed' },
    })
    expect(await updateUserSettings({ record_ip_log: true })).toEqual({
      success: false,
      message: 'Save failed',
    })
  })

  it('saving a stale notification form keeps the current IP setting and the notification edits', async () => {
    const onUpdate = vi.fn()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          ...profile,
          setting: JSON.stringify({ ...settings, record_ip_log: false }),
        },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    render(
      <NotificationTab
        profile={{ ...profile, setting: JSON.stringify(settings) }}
        onUpdate={onUpdate}
        quotaRemindEnabled
        quotaRemindThreshold={1000}
      />
    )
    expect(
      screen.queryByRole('switch', { name: 'Record IP Address' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('switch', {
        name: 'Receive New User Registration Notifications',
      })
    ).not.toBeInTheDocument()
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Reminder threshold (USD)' }),
      { target: { value: '0.0054' } }
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      ...settings,
      quota_warning_threshold: 2700,
      record_ip_log: false,
    })
  })

  it('an administrator can save and reload the registration subscription', async () => {
    const savedSettings = {
      ...settings,
      new_user_registration_notify_enabled: false,
    }
    const admin = {
      ...profile,
      role: 10,
      setting: JSON.stringify(savedSettings),
    }
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: admin },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const onUpdate = vi.fn()
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <NotificationTab
          profile={admin}
          onUpdate={onUpdate}
          quotaRemindEnabled
          quotaRemindThreshold={1000}
        />
      </QueryClientProvider>
    )
    const toggle = screen.getByRole('switch', {
      name: 'Receive New User Registration Notifications',
    })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)
    expect(
      screen.getByRole('button', { name: 'Send Test Notification' })
    ).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      ...savedSettings,
      new_user_registration_notify_enabled: true,
    })
    view.rerender(
      <QueryClientProvider client={client}>
        <NotificationTab
          profile={{
            ...admin,
            setting: JSON.stringify({
              ...settings,
              new_user_registration_notify_enabled: true,
            }),
          }}
          onUpdate={onUpdate}
          quotaRemindEnabled
          quotaRemindThreshold={1000}
        />
      </QueryClientProvider>
    )
    expect(toggle).toBeChecked()
    expect(
      screen.getByRole('button', { name: 'Send Test Notification' })
    ).toBeEnabled()
    client.clear()
  })

  it('sending a test uses saved configuration and handles pending, success and failure', async () => {
    const success = vi.spyOn(toast, 'success')
    const failure = vi.spyOn(toast, 'error')
    let resolveRequest!: (value: { data: { success: boolean } }) => void
    const post = vi.spyOn(api, 'post').mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve
        })
    )
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NotificationTab
          profile={{ ...profile, role: 100, setting: JSON.stringify(settings) }}
          onUpdate={vi.fn()}
          quotaRemindEnabled
          quotaRemindThreshold={1000}
        />
      </QueryClientProvider>
    )
    const button = screen.getByRole('button', {
      name: 'Send Test Notification',
    })
    fireEvent.click(button)
    await waitFor(() => expect(button).toBeDisabled())
    expect(post).toHaveBeenCalledWith(
      '/api/user/registration-notification/test'
    )
    resolveRequest({ data: { success: true } })
    await waitFor(() =>
      expect(success).toHaveBeenCalledWith('Test notification sent')
    )
    await waitFor(() => expect(button).toBeEnabled())
    post.mockResolvedValueOnce({
      data: { success: false, message: 'Notification unavailable' },
    })
    fireEvent.click(button)
    await waitFor(() => expect(failure).toHaveBeenCalled())
    expect(success).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(button).toBeEnabled())
    client.clear()
  })

  it('a failed test API response rejects instead of reporting success', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: 'Notification unavailable' },
    })
    await expect(sendRegistrationNotificationTest()).rejects.toThrow()
  })

  it('a demoted administrator can save ordinary settings and clears the old subscription', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          ...profile,
          setting: JSON.stringify({
            ...settings,
            new_user_registration_notify_enabled: true,
          }),
        },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await updateUserSettings({ record_ip_log: false })
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      ...settings,
      record_ip_log: false,
      new_user_registration_notify_enabled: false,
    })
  })
})

describe('notifications a user receives', () => {
  function renderNotificationTab(
    role: number,
    setting: Record<string, unknown>,
    quotaRemindEnabled = true
  ) {
    const onUpdate = vi.fn()
    render(
      <NotificationTab
        profile={{ ...profile, role, setting: JSON.stringify(setting) }}
        onUpdate={onUpdate}
        quotaRemindEnabled={quotaRemindEnabled}
        quotaRemindThreshold={1000}
      />
    )
    return onUpdate
  }

  function mockSave(role: number, setting: Record<string, unknown>) {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { ...profile, role, setting: JSON.stringify(setting) },
      },
    })
    return vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  }

  async function save(onUpdate: () => void) {
    fireEvent.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
  }

  it.each([
    { name: 'user', role: ROLE.USER, channel: undefined },
    { name: 'admin', role: ROLE.ADMIN, channel: false },
    { name: 'root', role: ROLE.SUPER_ADMIN, channel: true },
  ])(
    'a $name who never chose sees the reminder on and the channel switch as $channel',
    (row) => {
      renderNotificationTab(row.role, { notify_type: 'email' })
      expect(
        screen.getByRole('switch', { name: 'Low balance reminder' })
      ).toBeChecked()
      const channel = screen.queryByRole('switch', {
        name: 'Channel notifications',
      })
      const upstream = screen.queryByRole('switch', {
        name: 'Upstream Model Updates',
      })
      if (row.channel === undefined) {
        expect(channel).not.toBeInTheDocument()
        expect(upstream).not.toBeInTheDocument()
        return
      }
      expect(channel?.getAttribute('aria-checked')).toBe(String(row.channel))
      expect(upstream).not.toBeChecked()
    }
  )

  it('a user on a site with reminders off sees one line instead of notification settings', () => {
    renderNotificationTab(ROLE.USER, { notify_type: 'email' }, false)
    expect(
      screen.getByText('No notifications are available to you right now')
    ).toBeInTheDocument()
    expect(screen.queryByText('Notification Method')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('switch', { name: 'Low balance reminder' })
    ).not.toBeInTheDocument()
  })

  it('an admin on a site with reminders off keeps only the administrator notifications', () => {
    renderNotificationTab(ROLE.ADMIN, { notify_type: 'email' }, false)
    expect(
      screen.queryByRole('switch', { name: 'Low balance reminder' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('switch', { name: 'Channel notifications' })
    ).toBeInTheDocument()
    expect(screen.getByText('Notification Method')).toBeInTheDocument()
  })

  it('an empty threshold shows the site default and saves as the site default', async () => {
    const put = mockSave(ROLE.USER, { notify_type: 'email' })
    const onUpdate = renderNotificationTab(ROLE.USER, { notify_type: 'email' })
    const threshold = screen.getByRole('spinbutton', {
      name: 'Reminder threshold (USD)',
    })
    expect(threshold).toHaveValue(null)
    expect(threshold).toHaveAttribute('placeholder', '0.002')
    expect(
      screen.getByText('Leave empty to use the site default ($0.002)')
    ).toBeInTheDocument()
    await save(onUpdate)
    expect(sentBody(put)).toMatchObject({ quota_warning_threshold: 0 })
  })

  it('an untouched threshold is saved as stored, in quota units', async () => {
    const setting = { notify_type: 'email', quota_warning_threshold: 1234 }
    const put = mockSave(ROLE.USER, setting)
    const onUpdate = renderNotificationTab(ROLE.USER, setting)
    expect(
      screen.getByRole('spinbutton', { name: 'Reminder threshold (USD)' })
    ).toHaveValue(0.002468)
    await save(onUpdate)
    expect(sentBody(put)).toMatchObject({ quota_warning_threshold: 1234 })
  })

  it('turning the reminder off hides its threshold and saves the switch', async () => {
    const put = mockSave(ROLE.USER, { notify_type: 'email' })
    const onUpdate = renderNotificationTab(ROLE.USER, { notify_type: 'email' })
    fireEvent.click(
      screen.getByRole('switch', { name: 'Low balance reminder' })
    )
    expect(
      screen.queryByRole('spinbutton', { name: 'Reminder threshold (USD)' })
    ).not.toBeInTheDocument()
    await save(onUpdate)
    expect(sentBody(put)).toMatchObject({ quota_warning_enabled: false })
  })

  it('switches root never touched stay unset when it saves', async () => {
    const put = mockSave(ROLE.SUPER_ADMIN, { notify_type: 'email' })
    const onUpdate = renderNotificationTab(ROLE.SUPER_ADMIN, {
      notify_type: 'email',
    })
    await save(onUpdate)
    expect(sentBody(put)).not.toHaveProperty('channel_notify_enabled')
    expect(sentBody(put)).not.toHaveProperty('quota_warning_enabled')
  })

  it('an admin turning channel notifications on saves the switch', async () => {
    const put = mockSave(ROLE.ADMIN, { notify_type: 'email' })
    const onUpdate = renderNotificationTab(ROLE.ADMIN, { notify_type: 'email' })
    fireEvent.click(
      screen.getByRole('switch', { name: 'Channel notifications' })
    )
    await save(onUpdate)
    expect(sentBody(put)).toMatchObject({ channel_notify_enabled: true })
  })
})
