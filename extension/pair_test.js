const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'pair.js'), 'utf8'))

const parsed = TULAIPair.parse({ product: 'tulai', token: ' abc ', port: '18765' })
assert.deepStrictEqual(parsed, { token: 'abc', port: 18765 })
assert.throws(() => TULAIPair.parse({ product: 'other', token: 'abc', port: 1 }))
assert.throws(() => TULAIPair.parse({ product: 'tulai', token: '  ', port: 1 }))
assert.throws(() => TULAIPair.parse({ product: 'tulai', token: 'abc', port: 0 }))

async function main() {
  const pulled = await TULAIPair.pull(async () => ({
    ok: true,
    json: async () => ({ product: 'tulai', token: 'secret-token', port: 18770 }),
  }))
  assert.deepStrictEqual(pulled, { token: 'secret-token', port: 18770 })

  const saved = {}
  const storage = {
    async get() { return { profileId: saved.profileId || '' } },
    async set(values) { Object.assign(saved, values) },
  }
  const first = await TULAIPair.identity(storage)
  const second = await TULAIPair.identity(storage)
  assert.ok(first.profileId)
  assert.strictEqual(second.profileId, first.profileId)
  console.log('pair_test ok')
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
