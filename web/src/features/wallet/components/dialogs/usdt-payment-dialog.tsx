import { useMutation } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { checkUsdtPayment, type UsdtOrder } from '../../api'

export function UsdtPaymentDialog(props: {
  order: UsdtOrder
  onClose: () => void
  onPaid: () => void
}) {
  const { t } = useTranslation()
  const [remaining, setRemaining] = useState(() =>
    Math.max(0, props.order.expire_time - Math.floor(Date.now() / 1000))
  )
  const expired = remaining === 0
  const [pending, setPending] = useState(false)
  useEffect(() => {
    const timer = setInterval(
      () =>
        setRemaining(
          Math.max(0, props.order.expire_time - Math.floor(Date.now() / 1000))
        ),
      1000
    )
    return () => clearInterval(timer)
  }, [props.order.expire_time])
  const check = useMutation({
    mutationFn: () => checkUsdtPayment(props.order.trade_no),
    onSuccess: (paid) => {
      if (paid) props.onPaid()
      else setPending(true)
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('USDT payment')}
      description={t('USDT confirmation notice')}
      footer={
        <Button disabled={check.isPending} onClick={() => check.mutate()}>
          {t('Check payment status')}
        </Button>
      }
    >
      <div className='space-y-4'>
        <p>
          {t('Topup Amount')}: {props.order.credited_amount.toFixed(3)} USD
        </p>
        <p className='text-sm'>
          {expired ? t('USDT expired notice') : t('USDT exact amount notice')}
        </p>
        {!expired && (
          <>
            <p
              role='alert'
              className='border-destructive/30 bg-destructive/10 text-destructive rounded-lg border p-3 text-sm font-medium'
            >
              {t('USDT payment deadline warning')}
            </p>
            <div className='flex items-center gap-2'>
              <span className='font-mono'>
                {props.order.amount.toFixed(3)} USDT
              </span>
              <CopyButton value={props.order.amount.toFixed(3)} />
            </div>
            <div className='flex items-center gap-2'>
              <span className='min-w-0 font-mono text-sm break-all'>
                {props.order.address}
              </span>
              <CopyButton value={props.order.address} />
            </div>
            <p>
              {t('Network')}: {props.order.network}
            </p>
            <p>
              {t('Order expires in')}: {Math.floor(remaining / 60)}:
              {String(remaining % 60).padStart(2, '0')}
            </p>
            <p className='text-muted-foreground text-sm'>
              {t('USDT network fee notice')}
            </p>
          </>
        )}
        <p className='text-sm break-all'>
          {t('Order Number')}: {props.order.trade_no}
        </p>
        {pending && (
          <p role='status'>
            {expired
              ? t('USDT expired notice')
              : t('Payment not confirmed yet')}
          </p>
        )}
      </div>
    </Dialog>
  )
}
