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
import { afterEach, expect, test } from 'vitest'

import { toIntlLocale } from '@/i18n/languages'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { formatCurrencyFromUSD, formatQuotaWithCurrency } from '../currency'
import { parseQuotaFromDollars } from '../format'

afterEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

test.each([
  [0.009862, '$0.0098'],
  [0.0986, '$0.0986'],
  [1.99999, '$1.99'],
  [-1.99999, '-$1.99'],
  [0.000002, '$0'],
  [0, '$0'],
])(
  'truncating currency %s preserves the chosen decimal precision',
  (amount, expected) => {
    expect(
      formatCurrencyFromUSD(amount, {
        locale: 'en-US',
        roundingMode: 'trunc',
        abbreviate: false,
      })
    ).toBe(expected)
  }
)

test.each([
  ['zhCN', '$0.0098'],
  ['zhTW', '$0.0098'],
  ['en', '$0.0098'],
  ['fr', '0,0098\u00a0$'],
  ['ru', '0,0098\u00a0$'],
  ['ja', '$0.0098'],
  ['vi', '0,0098\u00a0$'],
  ['invalid_locale', '$0.0098'],
])(
  'truncated quota display supports interface locale %s',
  (language, expected) => {
    expect(
      formatQuotaWithCurrency(4931, {
        locale: toIntlLocale(language),
        roundingMode: 'trunc',
        abbreviate: false,
      })
    ).toBe(expected)
  }
)

test('truncation applies after currency conversion and leaves default formatting unchanged', () => {
  expect(formatCurrencyFromUSD(0.009862, { locale: 'en-US' })).toBe('$0.0099')
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7,
    },
  })
  expect(
    formatQuotaWithCurrency(4931, {
      locale: 'en-US',
      roundingMode: 'trunc',
    })
  ).toBe('¥0.069')
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CUSTOM',
      customCurrencySymbol: 'X',
      customCurrencyExchangeRate: 7,
    },
  })
  expect(
    formatQuotaWithCurrency(4931, {
      locale: 'en-US',
      roundingMode: 'trunc',
      showSymbol: false,
    })
  ).toBe('0.069')
})

test('token and compact displays truncate without changing the stored quota', () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'TOKENS',
    },
  })
  expect(formatQuotaWithCurrency(1999, { roundingMode: 'trunc' })).toBe('1.9k')
  expect(
    formatQuotaWithCurrency(1999, {
      locale: 'en-US',
      roundingMode: 'trunc',
      compact: true,
    })
  ).toBe('1.9K')
  expect(
    formatQuotaWithCurrency(4931, {
      roundingMode: 'trunc',
      abbreviate: false,
    })
  ).toBe('4931')
})

test.each([
  [0.0009059, 452],
  [0.0000019, 0],
  [0.009862, 4931],
  [-0.0009059, -452],
  [1e-8, 0],
  [Infinity, 0],
  [Number.NaN, 0],
])('manual amount %s truncates to %s quota units', (amount, quota) => {
  expect(parseQuotaFromDollars(amount, 'trunc')).toBe(quota)
})

test('decimal input conversion does not lose a quota unit to binary floating point', () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaPerUnit: 100,
    },
  })
  expect(parseQuotaFromDollars(0.29, 'trunc')).toBe(29)
  expect(parseQuotaFromDollars(0.299, 'trunc')).toBe(29)
  expect(parseQuotaFromDollars(0.299)).toBe(30)
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CUSTOM',
      customCurrencyExchangeRate: 3,
    },
  })
  expect(parseQuotaFromDollars(0.029586, 'trunc')).toBe(4931)
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'TOKENS',
    },
  })
  expect(parseQuotaFromDollars(4931.9, 'trunc')).toBe(4931)
})
