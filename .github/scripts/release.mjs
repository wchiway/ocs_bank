import { spawnSync } from 'node:child_process';
import { appendFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { previousTag, replaceSummary, summarize, validateChannel } from './release-lib.mjs';

const stateDir = resolve('.release');
function run(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: 'utf8', maxBuffer: 8 * 1024 * 1024, ...options });
  if (result.error || result.status !== 0) {
    const error = new Error(`${command} command failed`);
    error.notFound = command === 'gh' && /HTTP 404/.test(result.stderr ?? '');
    throw error;
  }
  return result.stdout.trim();
}
function api(path, method = 'GET', body) {
  return JSON.parse(run('gh', ['api', '--method', method, path, ...(body === undefined ? [] : ['--input', '-'])], { input: body === undefined ? undefined : JSON.stringify(body) }));
}
function repository() {
  const repo = process.env.GITHUB_REPOSITORY;
  if (!/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(repo ?? '')) throw new Error('GITHUB_REPOSITORY is required');
  return repo;
}
function metadata() { return JSON.parse(readFileSync(join(stateDir, 'metadata.json'), 'utf8')); }
function findRelease(repo, tag) {
  try { return api(`repos/${repo}/releases/tags/${encodeURIComponent(tag)}`); } catch (error) { if (error.notFound) return null; throw error; }
}
function assertOwned(release, meta) {
  if (release.draft || release.prerelease !== meta.prerelease || !release.body?.includes(meta.marker)) throw new Error('Existing release differs from this tag/commit/channel; refusing to overwrite it');
}
function prepare() {
  const tag = process.env.RELEASE_TAG, channel = process.env.RELEASE_CHANNEL;
  const prerelease = validateChannel(tag, channel);
  const commit = run('git', ['rev-parse', `${tag}^{commit}`]);
  if (commit !== run('git', ['rev-parse', 'HEAD'])) throw new Error('Checkout must match the version tag');
  run('git', ['merge-base', '--is-ancestor', commit, 'origin/main']);
  const tags = run('git', ['tag', '--merged', commit, '--list', 'v*']).split('\n').filter(Boolean);
  const previous = previousTag(tags, tag, channel);
  const range = previous ? `${previous}..${tag}` : tag;
  const commits = run('git', ['log', '--format=%h %s', '--reverse', range, '--']).split('\n').filter(Boolean);
  const repo = repository();
  const marker = `<!-- ocs-bank-release commit=${commit} -->`;
  let notes = `${marker}\n# ${tag}${prerelease ? '（预发布）' : ''}\n\n`;
  if (prerelease) notes += '> 这是预发布版本，建议先在测试环境验证。\n\n';
  notes += `## 变更记录\n\n${commits.length ? commits.map(x => `- ${x}`).join('\n') : '- 相较上一版本无新增提交。'}\n`;
  notes += previous ? `\n[完整比较](https://github.com/${repo}/compare/${previous}...${tag})\n` : `\n首次发布，以上记录来自当前 tag 可达的提交历史。\n`;
  if (notes.length > 40000) throw new Error('Release history is too long; create a curated intermediate release');
  notes = replaceSummary(notes, '版本总结待生成。源码下载由 GitHub 自动提供，原始变更记录如下。');
  mkdirSync(stateDir, { recursive: true });
  writeFileSync(join(stateDir, 'metadata.json'), JSON.stringify({ tag, channel, prerelease, commit, previous, marker }));
  writeFileSync(join(stateDir, 'notes.md'), notes);
  console.log(`Validated ${tag}; previous tag: ${previous ?? '(first release)'}`);
}
function publish() {
  const meta = metadata(), repo = repository();
  let release = findRelease(repo,meta.tag);
  if (release) assertOwned(release,meta);
  else release = api(`repos/${repo}/releases`, 'POST', {
    tag_name: meta.tag, target_commitish: meta.commit, name: meta.tag,
    body: readFileSync(join(stateDir,'notes.md'),'utf8'), draft:false, prerelease:meta.prerelease,
    make_latest: meta.prerelease ? 'false' : 'legacy',
  });
  console.log(`Published ${release.html_url}`);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY,`Release: ${release.html_url}\n`);
}
async function updateSummary() {
  const meta = metadata(), repo = repository();
  const release = findRelease(repo,meta.tag); if (!release) throw new Error('Release must be published before summarizing');
  assertOwned(release,meta);
  const source = readFileSync(join(stateDir,'notes.md'),'utf8');
  let summary;
  try { summary = await summarize(source, { apiKey: process.env.DEEPSEEK_API_KEY, model: process.env.DEEPSEEK_MODEL || 'deepseek-v4-flash' }); }
  catch (error) {
    if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY,'\nDeepSeek 总结失败；Release 和源码下载保留。配置密钥/额度后重跑 workflow 即可补全。\n');
    throw error;
  }
  const current = findRelease(repo,meta.tag);assertOwned(current,meta);
  api(`repos/${repo}/releases/${current.id}`,'PATCH',{body:replaceSummary(current.body,summary)});
  console.log('Updated release with DeepSeek summary');
}
try {
  switch (process.argv[2]) { case 'prepare':prepare();break;case 'publish':publish();break;case 'summarize':await updateSummary();break;default:throw new Error('Expected prepare, publish or summarize'); }
} catch (error) { console.error(error.message); process.exitCode=1; }
