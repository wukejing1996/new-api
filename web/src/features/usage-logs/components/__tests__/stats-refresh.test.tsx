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
import {
  focusManager,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { CommonLogsStats } from '../common-logs-stats'
import { UsageLogsProvider, useUsageLogsContext } from '../usage-logs-provider'

const clients: QueryClient[] = []

function StatsFixture() {
  const { setViewScope } = useUsageLogsContext()
  const list = useQuery({
    queryKey: ['logs', 'refresh-fixture'],
    queryFn: () => api.get('/api/log/?p=3'),
  })
  return (
    <>
      <CommonLogsStats />
      <button type='button' onClick={() => setViewScope('self')}>
        Only Mine
      </button>
      <p>{list.data ? 'Page 3 logs' : 'Loading logs'}</p>
    </>
  )
}

async function renderStats(role = 10, initialFailure = false) {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { quota: 100, rpm: 12, tpm: 345 } },
  })
  if (initialFailure) {
    get.mockRejectedValueOnce(new Error('Unavailable'))
  }
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: () => (
      <UsageLogsProvider>
        <StatsFixture />
      </UsageLogsProvider>
    ),
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({
      initialEntries: [
        '/usage-logs/common?page=3&username=alice&model=test-model&startTime=1700000000000&endTime=1700000060000',
      ],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  if (initialFailure) {
    await screen.findByRole('status')
  } else {
    await screen.findByText('345')
  }
  await screen.findByText('Page 3 logs')
  return { get, router }
}

async function selectInterval(name: string) {
  await userEvent.click(screen.getByRole('combobox', { name: 'Auto refresh' }))
  await userEvent.click(await screen.findByRole('option', { name }))
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  focusManager.setFocused(undefined)
  vi.useRealTimers()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.setUser(null)
})

it('manually refreshes statistics with applied filters without refetching logs or changing the page', async () => {
  const { get, router } = await renderStats()
  const location = router.state.location.href
  get.mockClear()
  get.mockResolvedValueOnce({
    data: { success: true, data: { quota: 200, rpm: 22, tpm: 678 } },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh statistics' })
  )
  await screen.findByText('678')
  expect(get).toHaveBeenCalledTimes(1)
  const url = new URL(String(get.mock.calls[0][0]), 'http://localhost')
  expect(url.pathname).toBe('/api/log/stat')
  expect(Object.fromEntries(url.searchParams)).toMatchObject({
    username: 'alice',
    model_name: 'test-model',
    start_timestamp: '1700000000',
    end_timestamp: '1700000060',
  })
  expect(router.state.location.href).toBe(location)
  expect(screen.getByText('Page 3 logs')).toBeVisible()
})

it('disables manual refresh and preserves values while a request is in progress', async () => {
  const { get } = await renderStats()
  let resolve!: (value: Awaited<ReturnType<typeof api.get>>) => void
  get.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done
      })
  )
  const button = screen.getByRole('button', { name: 'Refresh statistics' })
  await userEvent.click(button)
  expect(button).toBeDisabled()
  expect(button).toHaveAttribute('aria-busy', 'true')
  expect(screen.getByText('345')).toBeVisible()
  await userEvent.click(button)
  await act(async () =>
    resolve({ data: { success: true, data: { rpm: 0, tpm: 0, quota: 0 } } })
  )
  await waitFor(() => expect(button).toBeEnabled())
  expect(screen.getAllByText('0')).toHaveLength(2)
})

it('starts disabled, polls only statistics at the selected interval, changes intervals and stops when disabled', async () => {
  const { get } = await renderStats()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000)
  })
  expect(get).not.toHaveBeenCalled()
  await selectInterval('Every 10 seconds')
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000)
  })
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  expect(String(get.mock.calls[0][0])).toContain('/api/log/stat?')
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Refresh statistics' })
    ).toBeEnabled()
  )
  await selectInterval('Every 30 seconds')
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000)
  })
  expect(get).not.toHaveBeenCalled()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(20000)
  })
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  await selectInterval('Auto refresh: off')
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000)
  })
  expect(get).not.toHaveBeenCalled()
})

it('pauses polling when hidden and stops polling after the statistics are unmounted', async () => {
  const { get } = await renderStats()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  await selectInterval('Every 60 seconds')
  get.mockClear()
  act(() => focusManager.setFocused(false))
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000)
  })
  expect(get).not.toHaveBeenCalled()
  act(() => focusManager.setFocused(true))
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000)
  })
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  cleanup()
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000)
  })
  expect(get).not.toHaveBeenCalled()
})

it('stops administrator polling when switching to Only Mine', async () => {
  const { get } = await renderStats()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  await selectInterval('Every 10 seconds')
  await userEvent.click(screen.getByRole('button', { name: 'Only Mine' }))
  await waitFor(() =>
    expect(
      get.mock.calls.some(([url]) =>
        String(url).startsWith('/api/log/self/stat?')
      )
    ).toBe(true)
  )
  expect(
    screen.queryByRole('button', { name: 'Refresh statistics' })
  ).not.toBeInTheDocument()
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30000)
  })
  expect(get).not.toHaveBeenCalled()
})

it('does not expose administrator refresh controls in a regular user view', async () => {
  await renderStats(1)
  expect(
    screen.queryByRole('button', { name: 'Refresh statistics' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('combobox', { name: 'Auto refresh' })
  ).not.toBeInTheDocument()
})

it('shows unavailable values after an initial failure and recovers with manual refresh', async () => {
  await renderStats(10, true)
  expect(screen.getAllByText('—')).toHaveLength(3)
  expect(screen.getByRole('status')).toHaveTextContent('Refresh failed')
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh statistics' })
  )
  await screen.findByText('345')
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
})

it('preserves the previous statistics and displays an error when refreshing fails, then allows retry', async () => {
  const { get } = await renderStats()
  get.mockResolvedValueOnce({
    data: { success: false, message: 'Unavailable' },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh statistics' })
  )
  expect(await screen.findByRole('status')).toHaveTextContent('Refresh failed')
  expect(screen.getByText('345')).toBeVisible()
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh statistics' })
  )
  await waitFor(() =>
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  )
})
