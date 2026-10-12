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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RegistrationRisk } from '../index'
import type { RegistrationRiskSettings } from '../lib/schema'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

const endpoint = '/api/option/registration_risk'
let settings: RegistrationRiskSettings

function fixture(
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
) {
  render(
    <QueryClientProvider client={client}>
      <RegistrationRisk />
    </QueryClientProvider>
  )
  return client
}

beforeEach(() => {
  settings = {
    config: {
      enabled: false,
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
      ip_rule: { enabled: true, window_seconds: 3600, trigger_count: 2 },
      subnet_rule: { enabled: false, window_seconds: 3600, trigger_count: 3 },
      ipv4_prefix: 24,
      ipv6_prefix: 64,
    },
    api_key_configured: true,
    redis_enabled: true,
    target_limit_enabled: true,
    target_max_requests: 0,
  }
  vi.mocked(api.get).mockImplementation(async (path) => {
    let data: unknown = ['default', 'High Risk']
    if (path === endpoint) data = settings
    if (path === `${endpoint}/records`) data = { items: [], total: 0 }
    return { data: { success: true, data } }
  })
  vi.mocked(api.put).mockImplementation(async (_path, body) => {
    settings = {
      ...settings,
      config: (body as { config: RegistrationRiskSettings['config'] }).config,
    }
    return { data: { success: true, data: settings } }
  })
})

describe('registration risk settings', () => {
  it('reuses groups cached by other admin pages and preserves their response format', async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    const cachedGroups = { success: true, data: ['default', 'High Risk'] }
    client.setQueryData(['groups'], cachedGroups)
    fixture(client)

    const group = await screen.findByRole('combobox', { name: 'Risk user group' })
    await waitFor(() => expect(group).toHaveValue('High Risk'))
    expect(screen.getByRole('option', { name: 'default' })).toBeVisible()
    expect(screen.getByRole('option', { name: 'High Risk' })).toBeVisible()

    await client.invalidateQueries({ queryKey: ['groups'] })
    expect(client.getQueryData(['groups'])).toEqual(cachedGroups)
    expect(group).toHaveValue('High Risk')
  })

  it('saves selected rules while preserving a blank configured API key', async () => {
    const user = userEvent.setup()
    fixture()
    const key = await screen.findByLabelText('proxycheck.io API key')
    expect(key).toHaveValue('')
    expect(key).toHaveAttribute('type', 'password')
    await user.click(
      screen.getByRole('switch', { name: 'Enable new user risk control' })
    )
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Risk score at or above' }),
      { target: { value: '70' } }
    )
    await user.click(screen.getByRole('button', { name: 'Save risk settings' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith(endpoint, {
        config: { ...settings.config, enabled: true, risk_threshold: 70 },
      })
    )
    expect(
      screen.getByRole('button', { name: 'Save risk settings' })
    ).toBeDisabled()
  })

  it('preserves edits on a failed save and allows retry', async () => {
    const user = userEvent.setup()
    vi.mocked(api.put).mockRejectedValueOnce(new Error('save failed'))
    fixture()
    const threshold = await screen.findByRole('spinbutton', {
      name: 'Risk score at or above',
    })
    fireEvent.change(threshold, { target: { value: '65' } })
    await user.click(screen.getByRole('button', { name: 'Save risk settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('save failed')
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save risk settings' })
      ).toBeEnabled()
    )
    expect(threshold).toHaveValue(65)
    await user.click(screen.getByRole('button', { name: 'Save risk settings' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save risk settings' })
      ).toBeDisabled()
    )
  })

  it('background refresh preserves unsaved edits and secrets stay local to the form', async () => {
    const client = fixture()
    const key = await screen.findByLabelText('proxycheck.io API key')
    fireEvent.change(key, { target: { value: 'replacement-key' } })
    await client.invalidateQueries({ queryKey: ['registration-risk-settings'] })
    expect(key).toHaveValue('replacement-key')
    expect(
      client.getQueryData(['registration-risk-settings'])
    ).not.toHaveProperty('api_key')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('rejects an out-of-range trigger and exposes the validation error', async () => {
    fixture()
    const trigger = await screen.findByRole('spinbutton', {
      name: 'IP account trigger count',
    })
    fireEvent.change(trigger, { target: { value: '1' } })
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Enter an integer between 2 and 100'
    )
    expect(trigger).toHaveAttribute('aria-invalid', 'true')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('requires a selected group when enabling protection', async () => {
    const user = userEvent.setup()
    fixture()
    await screen.findByLabelText('Risk user group')
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Risk user group' }),
      ''
    )
    await user.click(
      screen.getByRole('switch', { name: 'Enable new user risk control' })
    )
    await user.click(screen.getByRole('button', { name: 'Save risk settings' }))
    expect(
      await screen.findByText('Select a risk user group', {
        selector: '[data-slot="form-message"]',
      })
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('tests saved intelligence without saving configuration', async () => {
    const user = userEvent.setup()
    vi.mocked(api.post).mockResolvedValue({
      data: {
        success: true,
        data: { restricted: true, incomplete: false, reasons: ['proxy_ip'] },
      },
    })
    fixture()
    await screen.findByLabelText('IP address')
    await user.type(screen.getByLabelText('IP address'), '8.8.4.4')
    await user.type(screen.getByLabelText('Email'), 'user@example.com')
    await user.click(screen.getByRole('button', { name: 'Run risk test' }))
    expect(
      await screen.findByText('Restricted', { selector: 'p' })
    ).toBeVisible()
    expect(api.post).toHaveBeenCalledWith(`${endpoint}/test`, {
      ip: '8.8.4.4',
      email: 'user@example.com',
    })
    expect(api.put).not.toHaveBeenCalled()
  })

  it('disables intelligence testing without a configured API key', async () => {
    settings.api_key_configured = false
    fixture()
    const ip = await screen.findByLabelText('IP address')
    fireEvent.change(ip, { target: { value: '8.8.4.4' } })
    expect(screen.getByRole('button', { name: 'Run risk test' })).toBeDisabled()
  })

  it('shows a failed-load state and reloads on retry', async () => {
    const user = userEvent.setup()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('unavailable'))
    fixture()
    expect(
      await screen.findByText('Failed to load risk settings')
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByLabelText('Risk user group')).toBeVisible()
  })
})
