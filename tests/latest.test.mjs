import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { eligibleLatest, promoteLatest } from '../.github/scripts/promote-latest.mjs';

test('latest promotion respects semantic versions and permits same-version retries', () => {
  const tags = ['v1.9.0', 'v1.10.0', 'v2.0.0-rc.1', 'invalid'];
  assert.equal(eligibleLatest('v1.9.0', tags), false);
  assert.equal(eligibleLatest('v1.10.0', tags), true);
  assert.throws(() => eligibleLatest('v2.0.0-rc.1', tags));
  assert.throws(() => eligibleLatest('v1.11.0', tags));
});

test('promotion reads remote tags then copies the exact built digest', () => {
  const calls = [], digest = `sha256:${'a'.repeat(64)}`;
  const run = (command, args) => {
    calls.push([command, args]);
    return command === 'gh' ? 'v1.9.0\nv1.10.0\n' : '';
  };
  assert.equal(promoteLatest({ repo: 'Owner/Repo', tag: 'v1.10.0', digest, run }).promoted, true);
  assert.deepEqual(calls[0], ['gh', ['api', '--paginate', 'repos/Owner/Repo/tags?per_page=100', '--jq', '.[].name']]);
  assert.deepEqual(calls[1], ['docker', ['buildx', 'imagetools', 'create', '--tag', 'ghcr.io/owner/repo:latest', `ghcr.io/owner/repo@${digest}`]]);
  calls.length = 0;
  assert.equal(promoteLatest({ repo: 'Owner/Repo', tag: 'v1.9.0', digest, run }).promoted, false);
  assert.equal(calls.length, 1);
});

test('failed lookup, malformed input and failed copy do not report successful promotion', () => {
  const options = { repo: 'owner/repo', tag: 'v1.0.0', digest: `sha256:${'a'.repeat(64)}` };
  assert.throws(() => promoteLatest({ ...options, run: () => { throw new Error('lookup failed'); } }));
  assert.throws(() => promoteLatest({ ...options, digest: 'invalid', run: () => assert.fail('command called') }));
  assert.throws(() => promoteLatest({ ...options, run: name => { if (name === 'gh') return 'v1.0.0'; throw new Error('copy failed'); } }));
});

test('only stable publication wires a serialized promotion after the build', () => {
  const stable = readFileSync(new URL('../.github/workflows/package-release.yml', import.meta.url), 'utf8');
  const pre = readFileSync(new URL('../.github/workflows/package-pre-release.yml', import.meta.url), 'utf8');
  assert(stable.includes('promote-latest:\n    needs: publish'));
  assert(stable.includes('group: ghcr-latest-${{ github.repository }}'));
  assert(stable.includes('cancel-in-progress: false'));
  assert(stable.includes('IMAGE_DIGEST: ${{ needs.publish.outputs.digest }}'));
  assert(!pre.includes('promote-latest'));
});
