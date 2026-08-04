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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { RatioType } from '../../types'
import {
  getEffectiveResolutionSelections,
  type RatioDifferenceEntry,
  type ResolutionSelection,
} from '../upstream-ratio-sync-helpers'

function modelDiffs(
  fields: Partial<Record<RatioType, number | string>>
): Partial<Record<RatioType, RatioDifferenceEntry>> {
  return Object.fromEntries(
    Object.entries(fields).map(([ratioType, value]) => [
      ratioType,
      {
        current: null,
        upstreams: { upstream: value },
        confidence: { upstream: true },
      },
    ])
  ) as Partial<Record<RatioType, RatioDifferenceEntry>>
}

describe('getEffectiveResolutionSelections', () => {
  test('price selection drops prior ratio fields for the same model', () => {
    const differences = {
      'gpt-4': modelDiffs({
        model_ratio: 1,
        completion_ratio: 2,
        model_price: 0.03,
      }),
    }

    const selections: ResolutionSelection[] = [
      {
        model: 'gpt-4',
        ratioType: 'model_ratio',
        value: 1,
        sourceName: 'upstream',
      },
      {
        model: 'gpt-4',
        ratioType: 'completion_ratio',
        value: 2,
        sourceName: 'upstream',
      },
      {
        model: 'gpt-4',
        ratioType: 'model_price',
        value: 0.03,
        sourceName: 'upstream',
      },
    ]

    const effective = getEffectiveResolutionSelections(differences, selections)
    assert.deepEqual(
      effective.map((item) => item.ratioType).sort(),
      ['model_price']
    )
  })

  test('ratio selection drops prior price fields for the same model', () => {
    const differences = {
      'gpt-4': modelDiffs({
        model_ratio: 1,
        model_price: 0.03,
      }),
    }

    const selections: ResolutionSelection[] = [
      {
        model: 'gpt-4',
        ratioType: 'model_price',
        value: 0.03,
        sourceName: 'upstream',
      },
      {
        model: 'gpt-4',
        ratioType: 'model_ratio',
        value: 1,
        sourceName: 'upstream',
      },
    ]

    const effective = getEffectiveResolutionSelections(differences, selections)
    assert.deepEqual(
      effective.map((item) => item.ratioType).sort(),
      ['model_ratio']
    )
  })

  test('handles thousands of selections without quadratic blow-up', () => {
    const differences: Record<
      string,
      Partial<Record<RatioType, RatioDifferenceEntry>>
    > = {}
    const selections: ResolutionSelection[] = []

    for (let i = 0; i < 5000; i += 1) {
      const model = `model-${i}`
      differences[model] = modelDiffs({
        model_ratio: 1,
        model_price: 0.01,
      })
      selections.push(
        {
          model,
          ratioType: 'model_ratio',
          value: 1,
          sourceName: 'upstream',
        },
        {
          model,
          ratioType: 'model_price',
          value: 0.01,
          sourceName: 'upstream',
        }
      )
    }

    const started = performance.now()
    const effective = getEffectiveResolutionSelections(differences, selections)
    const elapsed = performance.now() - started

    assert.equal(effective.length, 5000)
    assert.ok(
      elapsed < 250,
      `expected O(n) bulk resolve under 250ms, took ${elapsed.toFixed(1)}ms`
    )
  })
})
