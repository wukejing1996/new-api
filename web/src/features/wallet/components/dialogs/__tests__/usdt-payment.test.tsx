import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { UsdtPaymentDialog } from '../usdt-payment-dialog'
import { requestUsdtPayment } from '../../../api'

const localeFiles = import.meta.glob<{ default: { translation: Record<string, string> } }>('/src/i18n/locales/*.json', { eager: true })

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

test('a BSC checkout sends the selected network and shows its address, QR and full amount', async () => {
  const order = { trade_no: 'bsc-order', address: '0x3932af6fdbc450446bdb4eb4d2d13335c42a0417', amount: 20.137, credited_amount: 20.137, network: 'BSC', expire_time: Math.floor(Date.now() / 1000) + 1800 }
  const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { message: 'success', data: order } })
  const result = await requestUsdtPayment(20, 'BSC')
  expect(post).toHaveBeenCalledWith('/api/user/usdt/request', { amount: 20, network: 'BSC' })
  render(<QueryClientProvider client={new QueryClient()}><UsdtPaymentDialog order={result} onClose={() => {}} onPaid={() => {}} /></QueryClientProvider>)
  expect(screen.getByRole('img', { name: 'USDT payment' })).toBeVisible()
  expect(screen.getByText(order.address)).toBeVisible()
  expect(screen.getByText('20.137 USDT')).toBeVisible()
  expect(screen.getByText('Topup Amount: 20.137 USD')).toBeVisible()
  expect(screen.getByText('Network: BSC')).toBeVisible()
  expect(screen.getByText('USDT network fee notice')).toBeVisible()
})

test('all seven languages render the BSC amount, fee and address instructions instead of translation key names', async () => {
  for (const locale of ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi']) {
    const text = localeFiles[`/src/i18n/locales/${locale}.json`].default.translation
    i18next.addResourceBundle(locale, 'translation', text, true, true)
    await i18next.changeLanguage(locale)
    try {
      render(<QueryClientProvider client={new QueryClient()}><UsdtPaymentDialog order={{ trade_no: 'bsc-localized', address: '0x3932af6fdbc450446bdb4eb4d2d13335c42a0417', amount: 20.137, credited_amount: 20.137, network: 'BSC', expire_time: Math.floor(Date.now() / 1000) + 1800 }} onClose={() => {}} onPaid={() => {}} /></QueryClientProvider>)
      expect(screen.getByText(text['USDT exact amount notice'], { exact: true })).toBeVisible()
      expect(screen.getByText(text['USDT network fee notice'], { exact: true })).toBeVisible()
      expect(screen.getByText(text['USDT address QR notice'].replace('{{network}}', 'BSC (BEP20)'), { exact: true })).toBeVisible()
      expect(screen.getByRole('alert')).toHaveTextContent(text['USDT payment deadline warning'])
    } finally {
      cleanup()
      i18next.removeResourceBundle(locale, 'translation')
      await i18next.changeLanguage('en')
    }
  }
})
