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
  it('saves the enable toggle and edited values without clearing a stored API key', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    render(<Fixture />)
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'USDT' }))
    await user.click(screen.getByRole('switch', { name: 'Enable USDT' }))
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
