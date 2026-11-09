import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const root = new URL('../', import.meta.url);

test('headless distribution has no desktop entry, assets, packaging or guide', () => {
  for (const path of ['cmd/desktop', 'internal/desktop', 'mygo.json', 'docs/desktop.md', '.github/workflows/desktop.yml']) {
    assert.equal(existsSync(new URL(path, root)), false, path);
  }
  for (const path of ['cmd/server/main.go', 'cmd/maintain/main.go', 'cmd/migrate/main.go', 'Dockerfile', '.github/workflows/release.yml']) {
    assert.equal(existsSync(new URL(path, root)), true, path);
  }
});

test('Go modules and active docs do not depend on the desktop runtime', () => {
  for (const path of ['go.mod', 'README.md', 'AGENTS.md', 'docs/maintenance.md', 'docs/releases.md']) {
    const text = readFileSync(new URL(path, root), 'utf8');
    assert.doesNotMatch(text, /egoist\/mygo|cmd\/desktop|internal\/desktop|mygo\.json|docs\/desktop\.md/);
  }
});
