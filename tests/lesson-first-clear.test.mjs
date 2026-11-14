import { strict as assert } from 'node:assert'
import { test } from 'node:test'
import { isFirstClear } from '../client/app/src/courseProgress.ts'

test('levels beyond the completed count still earn a first clear', () => {
  assert.equal(isFirstClear({ completed: 3, unearnedLevels: [] }, 3), true)
  assert.equal(isFirstClear({ completed: 3, unearnedLevels: [] }, 10), true)
  assert.equal(isFirstClear({ completed: 0, unearnedLevels: [] }, 0), true)
})

test('replaying a completed level never pays first-clear rewards twice', () => {
  assert.equal(isFirstClear({ completed: 3, unearnedLevels: [] }, 0), false)
  assert.equal(isFirstClear({ completed: 3, unearnedLevels: [] }, 2), false)
})

test('levels added to an unlocked chapter count as uncompleted, not as replays', () => {
  assert.equal(isFirstClear({ completed: 65, unearnedLevels: [3, 4] }, 3), true)
  assert.equal(isFirstClear({ completed: 65, unearnedLevels: [3, 4] }, 4), true)
  assert.equal(isFirstClear({ completed: 65, unearnedLevels: [3, 4] }, 2), false)
})
