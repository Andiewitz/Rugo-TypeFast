import { strict as assert } from 'node:assert'
import { test } from 'node:test'
import { mostMissedKey, describeKey } from '../client/app/src/components/lesson/mistakes.ts'

test('identifies the most missed expected key', () => {
  assert.equal(mostMissedKey({ f: 1, j: 3 }), 'j')
  assert.equal(mostMissedKey({}), undefined)
})
test('describes whitespace as a key name', () => {
  assert.equal(describeKey(' '), 'space')
  assert.equal(describeKey('j'), 'J')
  assert.equal(describeKey('\n'), 'Enter')
  assert.equal(describeKey('\t'), 'Tab')
})
test('equal counts report the first recorded key', () => {
  assert.equal(mostMissedKey({ f: 2, j: 2 }), 'f')
})
