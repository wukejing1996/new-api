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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import { getTicketImage } from '../api'
import type { TicketImage as ImageData } from '../types'

export function TicketImage(props: { image: ImageData }) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState(false)
  const [image, setImage] = useState<{ blob: Blob; src: string } | null>(null)
  const query = useQuery({
    queryKey: ['admin-ticket-image', props.image.id],
    queryFn: ({ signal }) => getTicketImage(props.image.id, signal),
    staleTime: 5 * 60 * 1000,
  })
  useEffect(() => {
    if (!query.data) return
    const src = URL.createObjectURL(query.data)
    setImage({ blob: query.data, src })
    return () => URL.revokeObjectURL(src)
  }, [query.data])

  if (query.isError) {
    return (
      <Button variant='outline' onClick={() => void query.refetch()}>
        {t('Retry')}
      </Button>
    )
  }
  if (!image || image.blob !== query.data) {
    return (
      <span className='text-muted-foreground text-sm'>{t('Loading...')}</span>
    )
  }
  return (
    <>
      <Button
        variant='outline'
        className='h-auto max-w-48 overflow-hidden p-1'
        onClick={() => setPreview(true)}
        aria-label={t('View image')}
      >
        <img
          src={image.src}
          alt={props.image.file_name}
          className='max-h-32 max-w-full rounded object-contain'
        />
      </Button>
      <Dialog
        open={preview}
        onOpenChange={setPreview}
        title={t('Image preview')}
        contentClassName='sm:max-w-4xl'
      >
        <img
          src={image.src}
          alt={props.image.file_name}
          className='max-h-[75dvh] max-w-full object-contain'
        />
      </Dialog>
    </>
  )
}
