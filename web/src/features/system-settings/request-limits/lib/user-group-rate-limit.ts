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

export const periodUnits = { seconds: 1, minutes: 60, hours: 3600, days: 86400 }

const storedConfigSchema = z.object({
  enabled: z.boolean(),
  groups: z.record(
    z.string(),
    z.object({
      duration_seconds: z.number().int().min(1).max(2592000),
      max_requests: z.number().int().min(1).max(10000),
      custom_response_enabled: z.boolean().optional(),
      custom_response_message: z.string().optional(),
    })
  ),
})

export function createUserGroupRateLimitSchema(t: (key: string) => string) {
  return z
    .object({
      enabled: z.boolean(),
      rules: z
        .array(
          z.object({
            group: z.string().trim().min(1, t('Group name is required')),
            period: z.number().int().min(1, t('Enter a positive integer')),
            unit: z.enum(['seconds', 'minutes', 'hours', 'days']),
            maxRequests: z.number().int().min(1).max(10000),
            customResponseEnabled: z.boolean(),
            customResponseMessage: z
              .string()
              .refine(
                (value) => [...value].length <= 4000,
                t('Custom response must be at most 4000 characters.')
              ),
          })
        )
        .max(1000),
    })
    .superRefine((values, ctx) => {
      const groups = new Set<string>()
      values.rules.forEach((rule, index) => {
        if (groups.has(rule.group)) {
          ctx.addIssue({
            code: 'custom',
            path: ['rules', index, 'group'],
            message: t('Each user group can have only one rate limit rule.'),
          })
        }
        groups.add(rule.group)
        if (rule.customResponseEnabled && !rule.customResponseMessage.trim()) {
          ctx.addIssue({
            code: 'custom',
            path: ['rules', index, 'customResponseMessage'],
            message: t('Custom response message is required.'),
          })
        }
        if (rule.period * periodUnits[rule.unit] > 2592000) {
          ctx.addIssue({
            code: 'custom',
            path: ['rules', index, 'period'],
            message: t('The maximum period is 30 days.'),
          })
        }
      })
    })
}

export type UserGroupRateLimitFormValues = z.infer<
  ReturnType<typeof createUserGroupRateLimitSchema>
>

export function parseUserGroupRateLimit(
  raw: string
): UserGroupRateLimitFormValues {
  const stored = storedConfigSchema.parse(JSON.parse(raw))
  return {
    enabled: stored.enabled,
    rules: Object.entries(stored.groups).map(([group, rule]) => {
      const unit =
        (['days', 'hours', 'minutes', 'seconds'] as const).find(
          (candidate) => rule.duration_seconds % periodUnits[candidate] === 0
        ) ?? 'seconds'
      return {
        group,
        period: rule.duration_seconds / periodUnits[unit],
        unit,
        maxRequests: rule.max_requests,
        customResponseEnabled: rule.custom_response_enabled ?? false,
        customResponseMessage: rule.custom_response_message ?? '',
      }
    }),
  }
}

export function serializeUserGroupRateLimit(
  values: UserGroupRateLimitFormValues
): string {
  return JSON.stringify({
    enabled: values.enabled,
    groups: Object.fromEntries(
      values.rules.map((rule) => [
        rule.group,
        {
          duration_seconds: rule.period * periodUnits[rule.unit],
          max_requests: rule.maxRequests,
          ...(rule.customResponseEnabled && { custom_response_enabled: true }),
          ...(rule.customResponseMessage && {
            custom_response_message: rule.customResponseMessage,
          }),
        },
      ])
    ),
  })
}
