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
import { type TFunction } from 'i18next'
import { FILTER_ALL, QUOTA_TYPE_VALUES } from '../constants'
import {
  getDynamicPriceEntries,
  getDynamicPricingTiers,
  isDynamicPricingModel,
} from './dynamic-price'
import { parseTags } from './filters'
import { isTokenBasedModel } from './model-helpers'
import { formatFixedPrice, formatGroupPrice } from './price'
import type { PriceType, PricingModel, TokenUnit } from '../types'

type ExportPricingOptions = {
  models: PricingModel[]
  groupFilter: string
  groupRatio: Record<string, number>
  tokenUnit: TokenUnit
  priceRate: number
  usdExchangeRate: number
  t: TFunction
}

type CsvCell = string | number | null | undefined
type CsvRow = CsvCell[]

const PRICE_TYPES: { labelKey: string; type: PriceType }[] = [
  { labelKey: 'Input', type: 'input' },
  { labelKey: 'Output', type: 'output' },
  { labelKey: 'Cached input', type: 'cache' },
  { labelKey: 'Cache write', type: 'create_cache' },
  { labelKey: 'Image input', type: 'image' },
  { labelKey: 'Audio input', type: 'audio_input' },
  { labelKey: 'Audio output', type: 'audio_output' },
]

const DYNAMIC_FIELD_ORDER = [
  'inputPrice',
  'outputPrice',
  'cacheReadPrice',
  'cacheCreatePrice',
  'cacheCreate1hPrice',
  'imagePrice',
  'imageOutputPrice',
  'audioInputPrice',
  'audioOutputPrice',
]

function getDynamicFieldLabels(t: TFunction): Record<string, string> {
  return {
    inputPrice: t('Input'),
    outputPrice: t('Output'),
    cacheReadPrice: t('Cache Read'),
    cacheCreatePrice: t('Cache Write'),
    cacheCreate1hPrice: t('Cache Write (1h)'),
    imagePrice: t('Image input'),
    imageOutputPrice: t('Image output price'),
    audioInputPrice: t('Audio input'),
    audioOutputPrice: t('Audio output'),
  }
}

function getExportGroups(model: PricingModel, selectedGroup: string): string[] {
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []

  if (selectedGroup !== FILTER_ALL) {
    return groups.includes(selectedGroup) ? [selectedGroup] : []
  }

  return groups.length > 0 ? groups : ['']
}

function getGroupRatio(group: string, groupRatio: Record<string, number>) {
  if (!group) return 1
  return groupRatio[group] || 1
}

function getBillingMode(model: PricingModel, t: TFunction): string {
  if (isDynamicPricingModel(model)) return t('Dynamic Pricing')
  if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) return t('Per Request')
  return t('Token-based')
}

function formatTokenPrice(
  model: PricingModel,
  group: string,
  type: PriceType,
  options: ExportPricingOptions,
  showRechargePrice: boolean
): string {
  const value = formatGroupPrice(
    model,
    group,
    type,
    options.tokenUnit,
    showRechargePrice,
    options.priceRate,
    options.usdExchangeRate,
    options.groupRatio
  )

  return value === '-' ? '' : value
}

function buildStaticRows(
  model: PricingModel,
  group: string,
  options: ExportPricingOptions
): CsvRow[] {
  const isTokenBased = isTokenBasedModel(model)

  if (!isTokenBased) {
    return [
      [
        model.model_name,
        model.vendor_name || '',
        getBillingMode(model, options.t),
        group,
        getGroupRatio(group, options.groupRatio),
        options.t('Default'),
        options.t('Request price'),
        formatFixedPrice(
          model,
          group,
          false,
          options.priceRate,
          options.usdExchangeRate,
          options.groupRatio
        ),
        formatFixedPrice(
          model,
          group,
          true,
          options.priceRate,
          options.usdExchangeRate,
          options.groupRatio
        ),
        options.t('request'),
        model.supported_endpoint_types?.join(', ') || '',
        parseTags(model.tags).join(', '),
        model.enable_groups?.join(', ') || '',
        '',
      ],
    ]
  }

  return PRICE_TYPES.map(({ labelKey, type }) => {
    const officialPrice = formatTokenPrice(model, group, type, options, false)
    const discountedPrice = formatTokenPrice(model, group, type, options, true)
    if (!officialPrice && !discountedPrice) return null

    return [
      model.model_name,
      model.vendor_name || '',
      getBillingMode(model, options.t),
      group,
      getGroupRatio(group, options.groupRatio),
      options.t('Default'),
      options.t(labelKey),
      officialPrice,
      discountedPrice,
      `1${options.tokenUnit} ${options.t('tokens')}`,
      model.supported_endpoint_types?.join(', ') || '',
      parseTags(model.tags).join(', '),
      model.enable_groups?.join(', ') || '',
      '',
    ]
  }).filter((row): row is CsvRow => Boolean(row))
}

function buildDynamicRows(
  model: PricingModel,
  group: string,
  options: ExportPricingOptions
): CsvRow[] {
  const tiers = getDynamicPricingTiers(model)

  if (tiers.length === 0) {
    return [
      [
        model.model_name,
        model.vendor_name || '',
        options.t('Special billing expression'),
        group,
        getGroupRatio(group, options.groupRatio),
        options.t('Default'),
        options.t('Raw expression'),
        '',
        '',
        '',
        model.supported_endpoint_types?.join(', ') || '',
        parseTags(model.tags).join(', '),
        model.enable_groups?.join(', ') || '',
        model.billing_expr || '',
      ],
    ]
  }

  const fieldLabels = getDynamicFieldLabels(options.t)
  return tiers.flatMap((tier, tierIndex) => {
    const entries = getDynamicPriceEntries(tier, {
      tokenUnit: options.tokenUnit,
      showRechargePrice: false,
      priceRate: options.priceRate,
      usdExchangeRate: options.usdExchangeRate,
      groupRatioMultiplier: getGroupRatio(group, options.groupRatio),
    })
    const discountedEntries = getDynamicPriceEntries(tier, {
      tokenUnit: options.tokenUnit,
      showRechargePrice: true,
      priceRate: options.priceRate,
      usdExchangeRate: options.usdExchangeRate,
      groupRatioMultiplier: getGroupRatio(group, options.groupRatio),
    })

    return DYNAMIC_FIELD_ORDER.map((field) => {
      const entry = entries.find((item) => item.field === field)
      if (!entry) return null
      const discountedEntry = discountedEntries.find(
        (item) => item.field === field
      )

      return [
        model.model_name,
        model.vendor_name || '',
        getBillingMode(model, options.t),
        group,
        getGroupRatio(group, options.groupRatio),
        tier.label || `${options.t('Tier')} ${tierIndex + 1}`,
        fieldLabels[field] || field,
        entry.formatted,
        discountedEntry?.formatted || '',
        `1${options.tokenUnit} ${options.t('tokens')}`,
        model.supported_endpoint_types?.join(', ') || '',
        parseTags(model.tags).join(', '),
        model.enable_groups?.join(', ') || '',
        model.billing_expr || '',
      ]
    }).filter((row): row is CsvRow => Boolean(row))
  })
}

function csvEscape(value: CsvCell): string {
  const text = value == null ? '' : String(value)
  if (/[",\r\n]/.test(text)) {
    return `"${text.replace(/"/g, '""')}"`
  }
  return text
}

export function buildPricingCsv(options: ExportPricingOptions): string {
  const header = [
    options.t('Model'),
    options.t('Vendor'),
    options.t('Billing mode'),
    options.t('Group'),
    options.t('Group Ratio'),
    options.t('Tier'),
    options.t('Price type'),
    options.t('Official price'),
    options.t('Discounted price'),
    options.t('Unit'),
    options.t('Endpoints'),
    options.t('Tags'),
    options.t('Enabled groups'),
    options.t('Raw expression'),
  ]

  const rows = options.models.flatMap((model) =>
    getExportGroups(model, options.groupFilter).flatMap((group) =>
      isDynamicPricingModel(model)
        ? buildDynamicRows(model, group, options)
        : buildStaticRows(model, group, options)
    )
  )

  return [header, ...rows]
    .map((row) => row.map(csvEscape).join(','))
    .join('\r\n')
}

export function downloadCsv(filename: string, csv: string) {
  const blob = new Blob([`\uFEFF${csv}`], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

export function createPricingCsvFilename(groupFilter: string): string {
  const date = new Date().toISOString().slice(0, 10)
  const group = groupFilter === FILTER_ALL ? 'all-groups' : groupFilter
  const safeGroup = group.replace(/[^\w.-]+/g, '-')
  return `model-prices-${safeGroup}-${date}.csv`
}
