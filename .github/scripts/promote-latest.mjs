import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compareTags, parseTag, validateChannel } from './release-lib.mjs';

export function eligibleLatest(tag, tags) {
  validateChannel(tag, 'release');
  if (!tags.includes(tag)) throw new Error('Release tag is missing from the remote repository');
  return !tags.some(candidate => {
    let parsed;
    try { parsed = parseTag(candidate); } catch { return false; }
    return parsed.pre.length === 0 && compareTags(candidate, tag) > 0;
  });
}

// The workflow serializes this entire operation, including the fresh tag lookup.
export function promoteLatest({ repo, tag, digest, run = execFileSync }) {
  if (!/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(repo ?? '')) throw new Error('Invalid repository');
  validateChannel(tag, 'release');
  if (!/^sha256:[0-9a-f]{64}$/.test(digest ?? '')) throw new Error('Invalid image digest');
  const tags = run('gh', ['api', '--paginate', `repos/${repo}/tags?per_page=100`, '--jq', '.[].name'], {
    encoding: 'utf8', timeout: 30000, maxBuffer: 8 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'],
  }).trim().split(/\r?\n/).filter(Boolean);
  if (!eligibleLatest(tag, tags)) return { promoted: false, reason: 'newer_stable_tag' };
  const image = `ghcr.io/${repo.toLowerCase()}`;
  run('docker', ['buildx', 'imagetools', 'create', '--tag', `${image}:latest`, `${image}@${digest}`], {
    timeout: 120000, stdio: ['ignore', 'pipe', 'pipe'],
  });
  return { promoted: true, image: `${image}:latest`, digest };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const result = promoteLatest({ repo: process.env.GITHUB_REPOSITORY, tag: process.env.RELEASE_TAG, digest: process.env.IMAGE_DIGEST });
    const text = result.promoted ? `Promoted latest to ${result.digest}` : 'Skipped latest: a newer stable version tag exists';
    console.log(text);
    if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${text}\n`);
  } catch {
    console.error('Latest promotion failed; check release tag access and registry permissions');
    process.exitCode = 1;
  }
}
