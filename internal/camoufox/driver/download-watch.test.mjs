import assert from 'node:assert/strict';
import test from 'node:test';
import {watchDownload} from './download-watch.mjs';

test('stalled downloads fail so the installer can try another source', async () => {
  await assert.rejects(watchDownload(() => new Promise(() => {}), 25), /没有进度/);
});

test('progress extends the deadline and a completed transfer stops the timer', async () => {
  const result = await watchDownload(async progress => {
    for (let i = 0; i < 3; i++) {
      await new Promise(resolve => setTimeout(resolve, 15));
      progress();
    }
    return 'done';
  }, 80);
  assert.equal(result, 'done');
});
