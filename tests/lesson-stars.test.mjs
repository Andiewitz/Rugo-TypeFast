import { strict as assert } from 'node:assert'
import { test } from 'node:test'
import { starsFor, recordBestStars } from '../client/app/src/components/lesson/stars.ts'

test('stars reflect pass status and accuracy thresholds', () => {
  assert.equal(starsFor(false, 100), 0)
  assert.equal(starsFor(true, 89), 1)
  assert.equal(starsFor(true, 90), 2)
  assert.equal(starsFor(true, 98), 3)
})
test('replays preserve the best result', () => {
  assert.deepEqual(recordBestStars([3, 1], 0, 2), [3, 1])
  assert.deepEqual(recordBestStars([3, 1], 1, 3), [3, 3])
})
test('a first clear records stars for a level that has none yet', () => {
  assert.deepEqual(recordBestStars([3, 1], 2, 3), [3, 1, 3])
  assert.deepEqual(recordBestStars([], 0, 2), [2])
  const grown = recordBestStars([3], 3, 2)
  assert.equal(grown.length, 4)
  assert.equal(grown[3], 2)
})
