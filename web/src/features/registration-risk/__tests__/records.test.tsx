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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RiskRecords } from '../components/risk-records'
import type { RiskLog } from '../lib/records'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
const endpoint = '/api/option/registration_risk/records'

function entry(source = 'registration'): RiskLog {
  return {
    id: 1,
    event_id: 'risk-event',
    user_id: 8,
    username: 'new-account',
    created_at: 1760000000,
    ip: '8.8.4.4',
    user_agent: 'browser',
    success: true,
    request_id: 'request-risk',
    details: {
      source,
      assessment: {
        restricted: true,
        incomplete: false,
        reasons: source === 'registration' ? ['proxy_ip'] : ['ip_association'],
        ip_result:
          source === 'registration'
            ? {
                network: { type: 'Hosting' },
                detections: { risk: 80, proxy: true },
              }
            : undefined,
      },
      outcome: 'restricted',
      original_group: 'default',
      final_group: 'High Risk',
      account_created: true,
      group_applied: true,
      cache_synced: true,
      related_user_ids: [7, 8],
      trigger_user_id: source === 'association' ? 9 : 0,
      policy: {
        enabled: true,
        target_group: 'High Risk',
        proxy: true,
        vpn: true,
        tor: true,
        hosting: true,
        scraper: true,
        risk_score_enabled: true,
        risk_threshold: 50,
        disposable_email: true,
        fail_closed: true,
        timeout_seconds: 5,
        cache_seconds: 300,
        ipv4_prefix: 24,
        ipv6_prefix: 64,
        ip_rule: { enabled: true, window_seconds: 3600, trigger_count: 2 },
        subnet_rule: { enabled: false, window_seconds: 3600, trigger_count: 3 },
      },
    },
  }
}

function fixture() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RiskRecords />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: { items: [entry()], total: 21 } },
  })
})

describe('registration risk history', () => {
  it('shows risk signals, processing outcomes and immutable details', async () => {
    const user = userEvent.setup()
    fixture()
    expect(await screen.findByText('Proxy IP')).toBeVisible()
    expect(screen.getByText('Assigned to risk group')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Details' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('High Risk')).toBeVisible()
    expect(
      within(dialog).getByText('Cache synchronization completed')
    ).toBeVisible()
    expect(within(dialog).getByText(/"risk_threshold": 50/)).toBeVisible()
    expect(within(dialog).getByText(/"risk": 80/)).toBeVisible()
  })

  it('identifies related-account restrictions without presenting a new intelligence evaluation', async () => {
    const user = userEvent.setup()
    vi.mocked(api.get).mockResolvedValue({
      data: {
        success: true,
        data: { items: [entry('association')], total: 1 },
      },
    })
    fixture()
    await user.click(await screen.findByRole('button', { name: 'Details' }))
    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText('Related account restriction')
    ).toBeVisible()
    expect(within(dialog).getByText('Trigger user ID')).toBeVisible()
    expect(within(dialog).getByText('9')).toBeVisible()
    expect(
      within(dialog).queryByText('IP intelligence result')
    ).not.toBeInTheDocument()
  })

  it('filters by user ID and username and paginates on the server', async () => {
    const user = userEvent.setup()
    fixture()
    await screen.findByText('Proxy IP')
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(endpoint, {
        params: { p: 2, page_size: 20 },
      })
    )
    fireEvent.change(screen.getByRole('spinbutton', { name: 'User ID' }), {
      target: { value: '8' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: 'Username' }), {
      target: { value: 'new-account' },
    })
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(endpoint, {
        params: { p: 1, page_size: 20, user_id: 8, username: 'new-account' },
      })
    )
  })

  it('shows a failed query and retries without discarding filters', async () => {
    const user = userEvent.setup()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('storage offline'))
    fixture()
    expect(
      await screen.findAllByText('Failed to load audit records')
    ).not.toHaveLength(0)
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Proxy IP')).toBeVisible()
  })

  it('shows an empty history with no details action', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { items: [], total: 0 } },
    })
    fixture()
    expect(await screen.findByText('No records')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Details' })
    ).not.toBeInTheDocument()
  })
})
