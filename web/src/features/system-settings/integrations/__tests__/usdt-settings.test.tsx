import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { PaymentSettingsSection } from '../payment-settings-section'

const defaults: ComponentProps<typeof PaymentSettingsSection> = {
  defaultValues: {
    PayAddress: '',
    EpayId: '',
    EpayKey: '',
    Price: 1,
    MinTopUp: 1,
    CustomCallbackAddress: '',
    PayMethods: '[]',
    AmountOptions: '[]',
    AmountDiscount: '{}',
    StripeApiSecret: '',
    StripeWebhookSecret: '',
    StripePriceId: '',
    StripeUnitPrice: 1,
    StripeMinTopUp: 1,
    StripePromotionCodesEnabled: false,
    CreemApiKey: '',
    CreemWebhookSecret: '',
    CreemTestMode: false,
    CreemProducts: '[]',
  },
  waffoDefaultValues: {
    WaffoEnabled: false,
    WaffoApiKey: '',
    WaffoPrivateKey: '',
    WaffoPublicCert: '',
    WaffoSandboxPublicCert: '',
    WaffoSandboxApiKey: '',
    WaffoSandboxPrivateKey: '',
    WaffoSandbox: false,
    WaffoMerchantId: '',
    WaffoCurrency: 'USD',
    WaffoUnitPrice: 1,
    WaffoMinTopUp: 1,
    WaffoNotifyUrl: '',
    WaffoReturnUrl: '',
    WaffoPayMethods: '[]',
  },
  waffoPancakeDefaultValues: {
    WaffoPancakeMerchantID: '',
    WaffoPancakePrivateKey: '',
    WaffoPancakeReturnURL: '',
  },
  usdtDefaultValues: {
    UsdtEnabled: false,
    UsdtBscEnabled: false,
    UsdtBscReceiveAddress: '',
    UsdtBscRpcURL: '',
    UsdtMinTopUp: 10,
    UsdtReceiveAddress: 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t',
    UsdtTrongridApiKey: '',
    UsdtNetwork: 'TRC20',
    UsdtCheckInterval: 30,
    UsdtOrderExpireTime: 1800,
  },
  complianceDefaults: {
    confirmed: true,
    termsVersion: 'v1',
    confirmedAt: 0,
    confirmedBy: 1,
  },
}

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  )
  return (
    <QueryClientProvider client={client}>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <PaymentSettingsSection {...defaults} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

describe('USDT settings', () => {
  it('saves the BSC address and lookup URL before enabling BSC without changing TRC20', async () => {
    const updates: Array<{ key: string; value: unknown }> = []
    vi.spyOn(api, 'put').mockImplementation(async (_url, body) => {
      updates.push(body as { key: string; value: unknown })
      return { data: { success: true } }
    })
    render(<Fixture />)
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'USDT' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'BSC receive address' }), { target: { value: '0x3932af6fdbc450446bdb4eb4d2d13335c42a0417' } })
    fireEvent.change(screen.getByLabelText('BSC payment lookup URL'), { target: { value: 'https://example.com/test-key' } })
    await user.click(screen.getByRole('switch', { name: 'Enable BSC (BEP20) USDT' }))
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() => expect(updates).toContainEqual({ key: 'UsdtBscEnabled', value: true }))
    expect(updates.findIndex(v => v.key === 'UsdtBscReceiveAddress')).toBeLessThan(updates.findIndex(v => v.key === 'UsdtBscEnabled'))
    expect(updates.findIndex(v => v.key === 'UsdtBscRpcURL')).toBeLessThan(updates.findIndex(v => v.key === 'UsdtBscEnabled'))
    expect(updates.some(v => v.key === 'UsdtEnabled')).toBe(false)
    expect(updates.some(v => v.key === 'UsdtTrongridApiKey')).toBe(false)
  })
  it('saves the enable toggle and edited values without clearing a stored API key', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    render(<Fixture />)
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'USDT' }))
    await user.click(screen.getByRole('switch', { name: 'Enable USDT (TRC20)' }))
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Minimum Top-up' }),
      { target: { value: '25' } }
    )
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/option/', {
        key: 'UsdtEnabled',
        value: true,
      })
    )
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'UsdtMinTopUp',
      value: 25,
    })
    expect(put).not.toHaveBeenCalledWith(
      '/api/option/',
      expect.objectContaining({ key: 'UsdtTrongridApiKey' })
    )
  })
})
