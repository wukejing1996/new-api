import { useMutation } from '@tanstack/react-query'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeSVG } from 'qrcode.react'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'

import { checkUsdtPayment, type UsdtOrder } from '../../api'
import { getUsdtNetworkLabel } from '../../lib/payment'

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
  const ethereum = props.order.network === 'ETH'
  const requiresHash = ethereum || props.order.network === 'BSC'
  const networkLabel = getUsdtNetworkLabel(props.order.network)
  const expiredNotice = ethereum
    ? t('Ethereum order expired but can still be checked')
    : t('USDT expired notice')
  const hashHint = ethereum
    ? t('Paste the Ethereum transaction hash. Balance updates after final confirmation.')
    : t('Paste the BSC transaction hash.')
  const hashId = useId()
  const [txHash, setTxHash] = useState('')
  const [invalidHash, setInvalidHash] = useState(false)
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
    mutationFn: (hash: string) =>
      checkUsdtPayment(props.order.trade_no, props.order.network, hash),
    onSuccess: (paid) => {
      if (paid) props.onPaid()
      else setPending(true)
    },
    onError: (error) => handleServerError(error),
  })

  function checkPayment() {
    const hash = txHash.trim()
    if (requiresHash && !/^(0x)?[a-f0-9]{64}$/i.test(hash)) {
      setInvalidHash(true)
      return
    }
    setPending(false)
    check.mutate(hash)
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('USDT payment')}
      description={t('USDT confirmation notice')}
      footer={
        <Button disabled={check.isPending || (expired && props.order.network === 'BSC')} onClick={checkPayment}>
          {t('Check payment status')}
        </Button>
      }
    >
      <div className='space-y-4'>
        <p>
          {t('Topup Amount')}: {props.order.credited_amount.toFixed(3)} USD
        </p>
        <p className='text-sm'>
          {expired ? expiredNotice : t('USDT exact amount notice')}
        </p>
        {!expired && (
          <>
            <QRCodeSVG value={props.order.address} size={200} marginSize={4} className='mx-auto rounded-lg bg-white' role='img' aria-label={t('USDT payment')} />
            <p className='text-sm'>{t('USDT address QR notice', { network: networkLabel })}</p>
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
              {t('Network')}: {ethereum ? networkLabel : props.order.network}
            </p>
            <p>
              {t('Order expires in')}: {Math.floor(remaining / 60)}:
              {String(remaining % 60).padStart(2, '0')}
            </p>
            <p className='text-muted-foreground text-sm'>
              {ethereum
                ? t('Ethereum transfers may require ETH for gas. Network fees are paid separately.')
                : t('USDT network fee notice')}
            </p>
          </>
        )}
        <p className='text-sm break-all'>
          {t('Order Number')}: {props.order.trade_no}
        </p>
        {requiresHash && (!expired || ethereum) && (
          <div className='space-y-2'>
            <Label htmlFor={hashId}>{t('Transaction hash')}</Label>
            <Input
              id={hashId}
              value={txHash}
              onChange={(event) => {
                setTxHash(event.target.value)
                setInvalidHash(false)
              }}
              maxLength={66}
              autoComplete='off'
              spellCheck={false}
              aria-invalid={invalidHash || undefined}
              aria-describedby={`${hashId}-hint`}
            />
            <p id={`${hashId}-hint`} className='text-muted-foreground text-sm'>
              {invalidHash
                ? t('Enter a valid 64-character transaction hash.')
                : hashHint}
            </p>
          </div>
        )}
        {pending && (
          <p role='status'>
            {expired && !ethereum
              ? t('USDT expired notice')
              : t('Payment not confirmed yet')}
          </p>
        )}
      </div>
    </Dialog>
  )
}
