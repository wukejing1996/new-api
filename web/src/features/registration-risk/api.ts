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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { RiskLog, RiskRecordFilters } from './lib/records'
import {
  registrationRiskSchema,
  type RegistrationRiskConfig,
  type RegistrationRiskSettings,
  type RiskAssessment,
} from './lib/schema'

type Envelope<T> = { success: boolean; message: string; data: T }
const endpoint = '/api/option/registration_risk'

export async function getRegistrationRiskRecords(params: RiskRecordFilters) {
  const response = await api.get<Envelope<{ items: RiskLog[]; total: number }>>(
    `${endpoint}/records`,
    { params }
  )
  return requireServerSuccess(response.data).data
}

export async function getRegistrationRiskSettings() {
  const response = await api.get<Envelope<RegistrationRiskSettings>>(endpoint)
  const settings = requireServerSuccess(response.data).data
  return {
    ...settings,
    config: registrationRiskSchema.parse({
      ...settings.config,
      proxycheck_enabled: settings.config.proxycheck_enabled ?? true,
      ipapi_enabled: settings.config.ipapi_enabled ?? false,
      vpnapi_enabled: settings.config.vpnapi_enabled ?? false,
      abuser: settings.config.abuser ?? true,
      relay: settings.config.relay ?? true,
    }),
  }
}

export async function saveRegistrationRiskSettings(
  config: RegistrationRiskConfig,
  apiKey: string,
  ipapiKey = '',
  vpnapiKey = ''
) {
  const response = await api.put<Envelope<RegistrationRiskSettings>>(endpoint, {
    config,
    ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
    ...(ipapiKey.trim() ? { ipapi_key: ipapiKey.trim() } : {}),
    ...(vpnapiKey.trim() ? { vpnapi_key: vpnapiKey.trim() } : {}),
  })
  return requireServerSuccess(response.data).data
}

export async function testRegistrationRiskQuery(ip: string, email: string) {
  const response = await api.post<Envelope<RiskAssessment>>(
    `${endpoint}/test`,
    { ip, email }
  )
  return requireServerSuccess(response.data).data
}
