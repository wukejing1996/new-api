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
import type { TFunction } from 'i18next'

import type { RegistrationRiskConfig, RiskAssessment } from './schema'

export type RiskRecord = {
  source: string
  assessment: RiskAssessment
  policy: RegistrationRiskConfig
  outcome: string
  original_group: string
  final_group: string | null
  account_created: boolean
  group_applied: boolean
  cache_synced: boolean
  related_user_ids: number[] | null
  trigger_user_id: number
  processing_error?: string
}

export type RiskLog = {
  id: number
  event_id: string
  user_id: number
  username: string
  created_at: number
  ip: string
  user_agent: string
  request_id: string
  success: boolean
  details: RiskRecord
}

export type RiskRecordFilters = {
  p: number
  page_size: number
  user_id?: number
  username?: string
  request_id?: string
  start_timestamp?: number
  end_timestamp?: number
  success?: string
}

export function readRiskRecord(entry: RiskLog): RiskRecord | undefined {
  const record = entry.details
  if (!record || typeof record !== 'object') return undefined
  const candidate = record as RiskRecord
  if (!candidate.assessment || !Array.isArray(candidate.assessment.reasons)) {
    return undefined
  }
  return candidate
}

export function riskOutcomeLabel(
  value: string | undefined,
  t: TFunction
): string {
  switch (value) {
    case 'allowed':
      return t('Allowed in original group')
    case 'allowed_incomplete':
      return t('Allowed with incomplete checks')
    case 'restricted':
      return t('Assigned to risk group')
    case 'restricted_incomplete':
      return t('Restricted with incomplete checks')
    case 'registration_failed':
      return t('Account creation failed')
    case 'processing_failed':
      return t('Group update or cache sync failed')
    default:
      return t('Not recorded')
  }
}

export function riskReasonLabel(value: string, t: TFunction): string {
  switch (value) {
    case 'proxy_ip':
      return t('Proxy IP')
    case 'vpn_ip':
      return t('VPN IP')
    case 'tor_ip':
      return t('Tor exit node')
    case 'hosting_ip':
    case 'hosting_network':
      return t('Hosting or datacenter IP')
    case 'scraper_ip':
      return t('Known scraper IP')
    case 'ip_risk_score':
      return t('IP risk score threshold')
    case 'disposable_email':
      return t('Disposable email')
    case 'ip_association':
      return t('Same IP account threshold reached')
    case 'subnet_association':
      return t('Same subnet account threshold reached')
    case 'client_ip_unavailable':
      return t('Client IP unavailable')
    case 'ip_assessment_unavailable':
      return t('IP intelligence unavailable')
    case 'email_unavailable':
      return t('Email unavailable')
    case 'email_assessment_unavailable':
      return t('Email intelligence unavailable')
    case 'association_store_unavailable':
    case 'association_assessment_unavailable':
      return t('Association checks unavailable')
    case 'proxy_ip_unknown':
    case 'vpn_ip_unknown':
    case 'tor_ip_unknown':
    case 'hosting_ip_unknown':
    case 'scraper_ip_unknown':
    case 'network_type_unknown':
    case 'risk_score_unknown':
      return `${t('Required intelligence field missing')}: ${value}`
    default:
      return value
  }
}
