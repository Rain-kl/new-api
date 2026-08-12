import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToCreatePayload,
} from '../channel-form'

function validBase() {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'ratio-channel',
    models: 'gpt-4o',
    key: 'sk-test',
  }
}

describe('channel ratio form field', () => {
  test('defaults to 1 (no adjustment)', () => {
    assert.equal(CHANNEL_FORM_DEFAULT_VALUES.ratio, 1)
  })

  test('accepts 0 (free channel) and 100 (max)', () => {
    assert.equal(
      channelFormSchema.safeParse({ ...validBase(), ratio: 0 }).success,
      true
    )
    assert.equal(
      channelFormSchema.safeParse({ ...validBase(), ratio: 100 }).success,
      true
    )
  })

  test('rejects negative and above 100', () => {
    assert.equal(
      channelFormSchema.safeParse({ ...validBase(), ratio: -0.1 }).success,
      false
    )
    assert.equal(
      channelFormSchema.safeParse({ ...validBase(), ratio: 100.01 }).success,
      false
    )
  })

  test('sends ratio in the create payload', () => {
    const { channel } = transformFormDataToCreatePayload({
      ...validBase(),
      ratio: 0.8,
    })
    assert.equal(channel.ratio, 0.8)
  })
})
