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
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  createUserGroupRateLimitSchema,
  parseUserGroupRateLimit,
} from '../lib/user-group-rate-limit'
import { UserGroupRateLimitSection } from '../user-group-rate-limit-section'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), put: vi.fn() } }))

const empty = '{"enabled":false,"groups":{}}'

function Fixture(props: { initial?: string }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
  )
  return (
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <div ref={setActions} />
        <UserGroupRateLimitSection defaultValue={props.initial ?? empty} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.mocked(api.get).mockImplementation(async (path) => ({
    data: {
      success: true,
      data:
        path === '/api/option/user_group_rate_limit/stats'
          ? { redis_enabled: true, counts: {}, rejected_counts: {} }
          : ['default', 'High Risk'],
    },
  }))
  vi.mocked(api.put).mockResolvedValue({ data: { success: true } })
})

describe('user group rate limits', () => {
  it('queries cumulative custom replies and 429 counts and refreshes without saving edits', async () => {
    const user = userEvent.setup()
    let counts = {
      redis_enabled: true,
      counts: { 'High Risk': 7 },
      rejected_counts: { 'High Risk': 3, 'Former Group': 2 },
    }
    vi.mocked(api.get).mockImplementation(async (path) => ({
      data: {
        success: true,
        data:
          path === '/api/option/user_group_rate_limit/stats'
            ? counts
            : ['default', 'High Risk'],
      },
    }))
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}' />
    )
    const stats = screen.getByRole('region', { name: 'Rate limit counts' })
    expect(
      await within(stats).findByText('Total successful custom replies: 7')
    ).toBeVisible()
    expect(within(stats).getByText('Total 429 rejections: 5')).toBeVisible()
    const risk = within(stats).getByRole('row', { name: 'High Risk 7 3' })
    expect(risk).toBeVisible()
    expect(
      within(stats).getByRole('row', { name: 'Former Group 0 2' })
    ).toBeVisible()
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Limit Period' }), {
      target: { value: '2' },
    })
    counts = {
      redis_enabled: true,
      counts: { 'High Risk': 8 },
      rejected_counts: { 'High Risk': 4, 'Former Group': 2 },
    }
    await user.click(
      within(stats).getByRole('button', { name: 'Refresh counts' })
    )
    expect(
      await within(stats).findByText('Total successful custom replies: 8')
    ).toBeVisible()
    expect(within(stats).getByText('Total 429 rejections: 6')).toBeVisible()
    expect(
      screen.getByRole('spinbutton', { name: 'Limit Period' })
    ).toHaveValue(2)
    expect(api.put).not.toHaveBeenCalled()
  })

  it('shows unavailable statistics when Redis is disabled instead of displaying zero', async () => {
    vi.mocked(api.get).mockImplementation(async (path) => ({
      data: {
        success: true,
        data:
          path === '/api/option/user_group_rate_limit/stats'
            ? { redis_enabled: false, counts: {}, rejected_counts: {} }
            : ['default', 'High Risk'],
      },
    }))
    render(<Fixture />)
    expect(
      await screen.findByText(
        'Redis is not enabled. Rate limit counts are unavailable.'
      )
    ).toBeVisible()
    expect(
      screen.queryByText('Total 429 rejections: 0')
    ).not.toBeInTheDocument()
  })

  it('shows statistics query errors and allows a retry', async () => {
    const user = userEvent.setup()
    let offline = true
    vi.mocked(api.get).mockImplementation(async (path) => {
      if (path === '/api/option/user_group_rate_limit/stats' && offline) {
        throw new Error('Redis unavailable')
      }
      return {
        data: {
          success: true,
          data:
            path === '/api/option/user_group_rate_limit/stats'
              ? { redis_enabled: true, counts: {}, rejected_counts: {} }
              : ['default', 'High Risk'],
        },
      }
    })
    render(<Fixture />)
    const stats = screen.getByRole('region', { name: 'Rate limit counts' })
    expect(
      await within(stats).findByText('Failed to load rate limit counts')
    ).toBeVisible()
    expect(
      within(stats).queryByText('Total 429 rejections: 0')
    ).not.toBeInTheDocument()
    offline = false
    await user.click(within(stats).getByRole('button', { name: 'Retry' }))
    expect(
      await within(stats).findByText('No rate limit events counted yet.')
    ).toBeVisible()
  })
  it('validates and saves custom reply text only after the settings page Save', async () => {
    const user = userEvent.setup()
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}' />
    )
    await user.click(screen.getByRole('button', { name: 'Configure reply' }))
    const dialog = screen.getByRole('dialog')
    await user.click(
      within(dialog).getByRole('switch', {
        name: 'Enable custom over-limit reply',
      })
    )
    await user.click(within(dialog).getByRole('button', { name: 'Done' }))
    expect(api.put).not.toHaveBeenCalled()
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    expect(
      await screen.findByText('Custom response message is required.')
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Configure reply' }))
    fireEvent.change(
      within(screen.getByRole('dialog')).getByRole('textbox', {
        name: 'Custom reply text',
      }),
      { target: { value: '风险提示\n请联系支持' } }
    )
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Done' })
    )
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'UserGroupRateLimit',
        value: JSON.stringify({
          enabled: true,
          groups: {
            'High Risk': {
              duration_seconds: 3600,
              max_requests: 1,
              custom_response_enabled: true,
              custom_response_message: '风险提示\n请联系支持',
            },
          },
        }),
      })
    )
  })

  it('loads a saved custom reply and preserves its text when disabled', async () => {
    const user = userEvent.setup()
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1,"custom_response_enabled":true,"custom_response_message":"Saved reply"}}}' />
    )
    await user.click(screen.getByRole('button', { name: 'Configure reply' }))
    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).getByRole('textbox', { name: 'Custom reply text' })
    ).toHaveValue('Saved reply')
    expect(
      within(dialog).getByRole('switch', {
        name: 'Enable custom over-limit reply',
      })
    ).toBeChecked()
    await user.click(
      within(dialog).getByRole('switch', {
        name: 'Enable custom over-limit reply',
      })
    )
    await user.click(within(dialog).getByRole('button', { name: 'Done' }))
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'UserGroupRateLimit',
        value:
          '{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1,"custom_response_message":"Saved reply"}}}',
      })
    )
  })

  it('limits custom reply length and requires text only when enabled', () => {
    const schema = createUserGroupRateLimitSchema((key) => key)
    const values = parseUserGroupRateLimit(
      '{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}'
    )
    expect(schema.safeParse(values).success).toBe(true)
    values.rules[0].customResponseEnabled = true
    values.rules[0].customResponseMessage = ' \n\t'
    expect(schema.safeParse(values).success).toBe(false)
    values.rules[0].customResponseMessage = '字'.repeat(4001)
    expect(schema.safeParse(values).success).toBe(false)
    values.rules[0].customResponseMessage = '字'.repeat(4000)
    expect(schema.safeParse(values).success).toBe(true)
  })
  it('selects a user group and saves an independent one-hour rule', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    expect(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    ).toBeDisabled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add group' })).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: 'Add group' }))
    await user.click(screen.getByRole('combobox', { name: 'User Group' }))
    await user.click(screen.getByRole('option', { name: 'High Risk' }))
    expect(screen.getByRole('combobox', { name: 'Period Unit' })).toHaveValue(
      'Hours'
    )
    expect(
      screen.getByRole('spinbutton', { name: 'Limit Period' })
    ).toHaveValue(1)
    await user.click(
      screen.getByRole('switch', { name: 'Enable user group rate limits' })
    )
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'UserGroupRateLimit',
        value:
          '{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}',
      })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save user group rate limits' })
      ).toBeDisabled()
    )
  })

  it('loads a saved rule and persists deletion', async () => {
    const user = userEvent.setup()
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":7200,"max_requests":2}}}' />
    )
    expect(screen.getByRole('combobox', { name: 'User Group' })).toHaveValue(
      'High Risk'
    )
    expect(
      screen.getByRole('spinbutton', { name: 'Limit Period' })
    ).toHaveValue(2)
    expect(
      screen.getByRole('spinbutton', { name: 'Max Requests (incl. failures)' })
    ).toHaveValue(2)
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'UserGroupRateLimit',
        value: '{"enabled":true,"groups":{}}',
      })
    )
  })

  it('rejects periods above 30 days without saving', async () => {
    const user = userEvent.setup()
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":86400,"max_requests":1}}}' />
    )
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Limit Period' }), {
      target: { value: '31' },
    })
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    expect(
      await screen.findByText('The maximum period is 30 days.')
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('preserves edits when saving fails and allows a retry', async () => {
    vi.mocked(api.put).mockResolvedValueOnce({
      data: { success: false, message: 'Save rejected' },
    })
    const user = userEvent.setup()
    render(
      <Fixture initial='{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}' />
    )
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Max Requests (incl. failures)' }),
      { target: { value: '2' } }
    )
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    expect(
      screen.getByRole('spinbutton', { name: 'Max Requests (incl. failures)' })
    ).toHaveValue(2)
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save user group rate limits' })
      ).toBeEnabled()
    )
    await user.click(
      screen.getByRole('button', { name: 'Save user group rate limits' })
    )
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
  })

  it('offers a retry when group loading fails and disables adding rules', async () => {
    vi.mocked(api.get).mockImplementation(async (path) => {
      if (path === '/api/group/') throw new Error('Offline')
      return {
        data: {
          success: true,
          data: { redis_enabled: true, counts: {}, rejected_counts: {} },
        },
      }
    })
    render(<Fixture />)
    expect(await screen.findByText('Failed to load groups')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Add group' })).toBeDisabled()
  })

  it('refuses corrupt configuration instead of offering to save an empty replacement', () => {
    render(<Fixture initial='{"groups":null}' />)
    expect(
      screen.getByText('Failed to load user group rate limits')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Save user group rate limits' })
    ).not.toBeInTheDocument()
  })

  it('rejects duplicate groups and fractional limits', () => {
    const schema = createUserGroupRateLimitSchema((key) => key)
    const values = parseUserGroupRateLimit(
      '{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}'
    )
    values.rules.push({ ...values.rules[0] })
    expect(schema.safeParse(values).success).toBe(false)
    values.rules.pop()
    values.rules[0].maxRequests = 1.5
    expect(schema.safeParse(values).success).toBe(false)
  })
})
