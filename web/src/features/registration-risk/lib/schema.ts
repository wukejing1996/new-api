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
import { z } from 'zod'

const association = z.object({
  enabled: z.boolean(),
  window_seconds: z.number().int().min(1).max(86400),
  trigger_count: z.number().int().min(2).max(100),
})

export const registrationRiskSchema = z.object({
  enabled: z.boolean(),
  target_group: z.string().trim().max(64),
  proxycheck_enabled: z.boolean(),
  ipapi_enabled: z.boolean(),
  vpnapi_enabled: z.boolean(),
  abuser: z.boolean(),
  relay: z.boolean(),
  proxy: z.boolean(),
  vpn: z.boolean(),
  tor: z.boolean(),
  hosting: z.boolean(),
  scraper: z.boolean(),
  risk_score_enabled: z.boolean(),
  risk_threshold: z.number().int().min(0).max(100),
  disposable_email: z.boolean(),
  fail_closed: z.boolean(),
  timeout_seconds: z.number().int().min(1).max(15),
  cache_seconds: z.number().int().min(0).max(3600),
  ip_rule: association,
  subnet_rule: association,
  ipv4_prefix: z.number().int().min(16).max(32),
  ipv6_prefix: z.number().int().min(32).max(128),
})

export function createRegistrationRiskFormSchema(t: (key: string) => string) {
  return registrationRiskSchema
    .extend({
      api_key: z.string().max(256),
      ipapi_key: z.string().max(256),
      vpnapi_key: z.string().max(256),
    })
    .superRefine((value, ctx) => {
      if (value.enabled && !value.target_group) {
        ctx.addIssue({
          code: 'custom',
          path: ['target_group'],
          message: t('Select a risk user group'),
        })
      }
    })
}

export type RegistrationRiskConfig = z.infer<typeof registrationRiskSchema>
export type RegistrationRiskFormValues = RegistrationRiskConfig & {
  api_key: string
  ipapi_key: string
  vpnapi_key: string
}
export type RegistrationRiskSettings = {
  config: RegistrationRiskConfig
  api_key_configured: boolean
  ipapi_key_configured?: boolean
  vpnapi_key_configured?: boolean
  redis_enabled: boolean
  target_limit_enabled: boolean
  target_max_requests: number
}
export type RiskAssessment = {
  restricted: boolean
  incomplete: boolean
  reasons: string[]
  ip_result?: {
    network: { type: string | null }
    detections: Record<string, boolean | number | null>
  }
  disposable?: boolean
  providers?: {
    provider: string
    restricted: boolean
    incomplete: boolean
    reasons: string[]
    signals?: Record<string, boolean | null>
    risk?: number
    network_type?: string
    error?: string
  }[]
}
