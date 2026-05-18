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

type ExportCell = string | number | null | undefined
type ExportColumn = {
  key: string
  label: string
  group: 'meta' | 'official' | 'discounted' | 'extra'
}
type ExportRow = Record<string, ExportCell>
type ExportGroup = {
  name: string
  ratio: number
  label: string
}

const STATIC_PRICE_TYPES: {
  officialKey: string
  discountedKey: string
  type: PriceType
}[] = [
  { officialKey: 'official_input', discountedKey: 'discounted_input', type: 'input' },
  { officialKey: 'official_output', discountedKey: 'discounted_output', type: 'output' },
  {
    officialKey: 'official_cache_read',
    discountedKey: 'discounted_cache_read',
    type: 'cache',
  },
  {
    officialKey: 'official_cache_write',
    discountedKey: 'discounted_cache_write',
    type: 'create_cache',
  },
  {
    officialKey: 'official_image_input',
    discountedKey: 'discounted_image_input',
    type: 'image',
  },
  {
    officialKey: 'official_audio_input',
    discountedKey: 'discounted_audio_input',
    type: 'audio_input',
  },
  {
    officialKey: 'official_audio_output',
    discountedKey: 'discounted_audio_output',
    type: 'audio_output',
  },
]

const DYNAMIC_PRICE_FIELDS = [
  {
    field: 'inputPrice',
    officialKey: 'official_input',
    discountedKey: 'discounted_input',
  },
  {
    field: 'outputPrice',
    officialKey: 'official_output',
    discountedKey: 'discounted_output',
  },
  {
    field: 'cacheReadPrice',
    officialKey: 'official_cache_read',
    discountedKey: 'discounted_cache_read',
  },
  {
    field: 'cacheCreatePrice',
    officialKey: 'official_cache_write',
    discountedKey: 'discounted_cache_write',
  },
  {
    field: 'imagePrice',
    officialKey: 'official_image_input',
    discountedKey: 'discounted_image_input',
  },
  {
    field: 'audioInputPrice',
    officialKey: 'official_audio_input',
    discountedKey: 'discounted_audio_input',
  },
  {
    field: 'audioOutputPrice',
    officialKey: 'official_audio_output',
    discountedKey: 'discounted_audio_output',
  },
]

function buildColumns(t: TFunction): ExportColumn[] {
  return [
    { key: 'model', label: t('Model'), group: 'meta' },
    { key: 'vendor', label: t('Vendor'), group: 'meta' },
    { key: 'billing_mode', label: t('Billing mode'), group: 'meta' },
    { key: 'group', label: t('Group'), group: 'meta' },
    { key: 'group_ratio', label: t('Group Ratio'), group: 'meta' },
    { key: 'tier', label: t('Tier'), group: 'meta' },
    { key: 'unit', label: t('Unit'), group: 'meta' },
    {
      key: 'official_input',
      label: t('Official input price'),
      group: 'official',
    },
    {
      key: 'discounted_input',
      label: t('Discounted input price'),
      group: 'discounted',
    },
    {
      key: 'official_output',
      label: t('Official output price'),
      group: 'official',
    },
    {
      key: 'discounted_output',
      label: t('Discounted output price'),
      group: 'discounted',
    },
    {
      key: 'official_cache_read',
      label: t('Official cache read price'),
      group: 'official',
    },
    {
      key: 'discounted_cache_read',
      label: t('Discounted cache read price'),
      group: 'discounted',
    },
    {
      key: 'official_cache_write',
      label: t('Official cache write price'),
      group: 'official',
    },
    {
      key: 'discounted_cache_write',
      label: t('Discounted cache write price'),
      group: 'discounted',
    },
    {
      key: 'official_image_input',
      label: t('Official image input price'),
      group: 'official',
    },
    {
      key: 'discounted_image_input',
      label: t('Discounted image input price'),
      group: 'discounted',
    },
    {
      key: 'official_audio_input',
      label: t('Official audio input price'),
      group: 'official',
    },
    {
      key: 'discounted_audio_input',
      label: t('Discounted audio input price'),
      group: 'discounted',
    },
    {
      key: 'official_audio_output',
      label: t('Official audio output price'),
      group: 'official',
    },
    {
      key: 'discounted_audio_output',
      label: t('Discounted audio output price'),
      group: 'discounted',
    },
    {
      key: 'request_official',
      label: t('Official request price'),
      group: 'official',
    },
    {
      key: 'request_discounted',
      label: t('Discounted request price'),
      group: 'discounted',
    },
    { key: 'endpoints', label: t('Endpoints'), group: 'extra' },
    { key: 'tags', label: t('Tags'), group: 'extra' },
    { key: 'enabled_groups', label: t('Enabled groups'), group: 'extra' },
    { key: 'raw_expression', label: t('Raw expression'), group: 'extra' },
  ]
}

function getMinAvailableGroup(
  model: PricingModel,
  groupRatio: Record<string, number>,
  t: TFunction
): ExportGroup {
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []
  if (groups.length === 0) {
    return { name: '', ratio: 1, label: t('All Groups') }
  }

  let selected = groups[0]
  let selectedRatio = groupRatio[selected] || 1
  for (const group of groups.slice(1)) {
    const ratio = groupRatio[group] || 1
    if (ratio < selectedRatio) {
      selected = group
      selectedRatio = ratio
    }
  }

  return {
    name: selected,
    ratio: selectedRatio,
    label: `${t('All Groups')} (${selected})`,
  }
}

function getExportGroup(
  model: PricingModel,
  selectedGroup: string,
  options: ExportPricingOptions
): ExportGroup | null {
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []
  if (selectedGroup !== FILTER_ALL) {
    if (!groups.includes(selectedGroup)) return null
    return {
      name: selectedGroup,
      ratio: getGroupRatio(selectedGroup, options.groupRatio),
      label: selectedGroup,
    }
  }

  return getMinAvailableGroup(model, options.groupRatio, options.t)
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

function getBaseRow(
  model: PricingModel,
  group: ExportGroup,
  options: ExportPricingOptions,
  tier?: string
): ExportRow {
  return {
    model: model.model_name,
    vendor: model.vendor_name || '',
    billing_mode: getBillingMode(model, options.t),
    group: group.label,
    group_ratio: group.ratio,
    tier: tier || options.t('Default'),
    unit: isTokenBasedModel(model)
      ? `1${options.tokenUnit} ${options.t('tokens')}`
      : options.t('request'),
    endpoints: model.supported_endpoint_types?.join(', ') || '',
    tags: parseTags(model.tags).join(', '),
    enabled_groups: model.enable_groups?.join(', ') || '',
    raw_expression: '',
  }
}

function formatTokenPrice(
  model: PricingModel,
  group: ExportGroup,
  type: PriceType,
  options: ExportPricingOptions,
  showRechargePrice: boolean
): string {
  const value = formatGroupPrice(
    model,
    group.name,
    type,
    options.tokenUnit,
    showRechargePrice,
    options.priceRate,
    options.usdExchangeRate,
    options.groupRatio
  )

  return value === '-' ? '' : value
}

function buildStaticRow(
  model: PricingModel,
  group: ExportGroup,
  options: ExportPricingOptions
): ExportRow {
  const row = getBaseRow(model, group, options)

  if (!isTokenBasedModel(model)) {
    row.request_official = formatFixedPrice(
      model,
      group.name,
      false,
      options.priceRate,
      options.usdExchangeRate,
      options.groupRatio
    )
    row.request_discounted = formatFixedPrice(
      model,
      group.name,
      true,
      options.priceRate,
      options.usdExchangeRate,
      options.groupRatio
    )
    return row
  }

  for (const priceType of STATIC_PRICE_TYPES) {
    row[priceType.officialKey] = formatTokenPrice(
      model,
      group,
      priceType.type,
      options,
      false
    )
    row[priceType.discountedKey] = formatTokenPrice(
      model,
      group,
      priceType.type,
      options,
      true
    )
  }

  return row
}

function buildDynamicRows(
  model: PricingModel,
  group: ExportGroup,
  options: ExportPricingOptions
): ExportRow[] {
  const tiers = getDynamicPricingTiers(model)

  if (tiers.length === 0) {
    return [
      {
        ...getBaseRow(model, group, options),
        billing_mode: options.t('Special billing expression'),
        unit: '',
        raw_expression: model.billing_expr || '',
      },
    ]
  }

  return tiers.map((tier, tierIndex) => {
    const row = getBaseRow(
      model,
      group,
      options,
      tier.label || `${options.t('Tier')} ${tierIndex + 1}`
    )
    const officialEntries = getDynamicPriceEntries(tier, {
      tokenUnit: options.tokenUnit,
      showRechargePrice: false,
      priceRate: options.priceRate,
      usdExchangeRate: options.usdExchangeRate,
      groupRatioMultiplier: group.ratio,
    })
    const discountedEntries = getDynamicPriceEntries(tier, {
      tokenUnit: options.tokenUnit,
      showRechargePrice: true,
      priceRate: options.priceRate,
      usdExchangeRate: options.usdExchangeRate,
      groupRatioMultiplier: group.ratio,
    })

    for (const priceField of DYNAMIC_PRICE_FIELDS) {
      row[priceField.officialKey] =
        officialEntries.find((entry) => entry.field === priceField.field)
          ?.formatted || ''
      row[priceField.discountedKey] =
        discountedEntries.find((entry) => entry.field === priceField.field)
          ?.formatted || ''
    }

    row.raw_expression = model.billing_expr || ''
    return row
  })
}

function buildRows(options: ExportPricingOptions): ExportRow[] {
  return options.models.flatMap((model) =>
    (() => {
      const group = getExportGroup(model, options.groupFilter, options)
      if (!group) return []
      return isDynamicPricingModel(model)
        ? buildDynamicRows(model, group, options)
        : [buildStaticRow(model, group, options)]
    })()
  )
}

function escapeHtml(value: ExportCell): string {
  const text = value == null ? '' : String(value)
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function getHeaderStyle(group: ExportColumn['group']): string {
  switch (group) {
    case 'official':
      return 'background:#dbeafe;color:#1e3a8a;'
    case 'discounted':
      return 'background:#dcfce7;color:#14532d;'
    case 'extra':
      return 'background:#fef3c7;color:#78350f;'
    case 'meta':
    default:
      return 'background:#e5e7eb;color:#111827;'
  }
}

export function buildPricingSpreadsheet(options: ExportPricingOptions): string {
  const columns = buildColumns(options.t)
  const rows = buildRows(options)
  const headerCells = columns
    .map(
      (column) =>
        `<th style="${getHeaderStyle(column.group)}border:1px solid #9ca3af;padding:6px;font-weight:700;">${escapeHtml(column.label)}</th>`
    )
    .join('')
  const bodyRows = rows
    .map((row) => {
      const cells = columns
        .map(
          (column) =>
            `<td style="border:1px solid #d1d5db;padding:5px;mso-number-format:'\\@';">${escapeHtml(row[column.key])}</td>`
        )
        .join('')
      return `<tr>${cells}</tr>`
    })
    .join('')

  return `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
</head>
<body>
  <table>
    <thead><tr>${headerCells}</tr></thead>
    <tbody>${bodyRows}</tbody>
  </table>
</body>
</html>`
}

export function downloadSpreadsheet(filename: string, content: string) {
  const blob = new Blob([`\uFEFF${content}`], {
    type: 'application/vnd.ms-excel;charset=utf-8',
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

export function createPricingSpreadsheetFilename(groupFilter: string): string {
  const date = new Date().toISOString().slice(0, 10)
  const group = groupFilter === FILTER_ALL ? 'all-groups' : groupFilter
  const safeGroup = group.replace(/[^\w.-]+/g, '-')
  return `model-prices-${safeGroup}-${date}.xls`
}
