import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { UsdtPaymentDialog } from '../usdt-payment-dialog'

test('an expired USDT order hides transfer details but still permits payment reconciliation', async () => {
  const paid = vi.fn()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { message: 'pending', data: 'pending' } })
    .mockResolvedValueOnce({ data: { message: 'success', data: 'credited' } })
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsdtPaymentDialog
        order={{
          trade_no: 'order-one',
          address: 'TRON-address',
          amount: 10.137,
          credited_amount: 10.137,
          network: 'TRC20',
          expire_time: 1,
        }}
        onClose={() => {}}
        onPaid={paid}
      />
    </QueryClientProvider>
  )
  expect(screen.queryByText('TRON-address')).not.toBeInTheDocument()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Check payment status' }))
  expect(await screen.findByRole('status')).toHaveTextContent(
    'USDT expired notice'
  )
  expect(paid).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Check payment status' }))
  await waitFor(() => expect(paid).toHaveBeenCalledOnce())
  expect(post).toHaveBeenCalledWith('/api/user/usdt/check', {
    trade_no: 'order-one',
  })
})

test('USDT transfer amount retains all three decimals and exposes the credited USD amount', () => {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <UsdtPaymentDialog
        order={{
          trade_no: 'order-two',
          address: 'TRON-address',
          amount: 10.137,
          credited_amount: 10.137,
          network: 'TRC20',
          expire_time: Math.floor(Date.now() / 1000) + 1800,
        }}
        onClose={() => {}}
        onPaid={() => {}}
      />
    </QueryClientProvider>
  )
  expect(screen.getByText('10.137 USDT')).toBeVisible()
  expect(screen.getByText('Topup Amount: 10.137 USD')).toBeVisible()
  expect(screen.getByText('TRON-address')).toBeVisible()
  expect(screen.getByRole('alert')).toHaveTextContent(
    'USDT payment deadline warning'
  )
})
