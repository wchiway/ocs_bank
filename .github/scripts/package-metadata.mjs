import { appendFileSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { validateChannel } from './release-lib.mjs';

export function packageMetadata(repo, tag, channel, commit) {
  if (!/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(repo ?? '')) throw new Error('Invalid GitHub repository');
  validateChannel(tag, channel);
  if (tag.length > 128) throw new Error('Version exceeds container tag length limit');
  if (!/^[0-9a-f]{40}$/.test(commit ?? '')) throw new Error('Invalid release commit');
  return { image: `ghcr.io/${repo.toLowerCase()}:${tag}`, commit };
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const meta = JSON.parse(readFileSync('.release/metadata.json', 'utf8'));
    const output = packageMetadata(process.env.GITHUB_REPOSITORY, meta.tag, meta.channel, meta.commit);
    if (!process.env.GITHUB_OUTPUT) throw new Error('GITHUB_OUTPUT is required');
    for (const [key, value] of Object.entries(output)) appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`);
    if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `Container image: \`${output.image}\`\n`);
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
