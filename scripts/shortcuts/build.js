// Compiles the .scpl sources in this directory into .shortcut files served
// from static/shortcuts/. Each source has a %%HUB_URL%% placeholder so the
// same templates work for any deployment, not just this one, re-run this
// (npm install && npm run build, or `make build-shortcuts`) after editing a
// .scpl file, or whenever HUB_URL/PUBLIC_URL changes.
const fs = require('fs');
const path = require('path');
const { parse } = require('scpl');

const rootEnvPath = path.join(__dirname, '..', '..', '.env');

function loadRootEnv() {
  if (!fs.existsSync(rootEnvPath)) return {};
  const vars = {};
  for (const line of fs.readFileSync(rootEnvPath, 'utf8').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const eq = trimmed.indexOf('=');
    if (eq === -1) continue;
    vars[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim();
  }
  return vars;
}

const rootEnv = loadRootEnv();
const hubUrl = process.env.HUB_URL || process.env.PUBLIC_URL || rootEnv.HUB_URL || rootEnv.PUBLIC_URL;

if (!hubUrl) {
  console.error(
    'HUB_URL (or PUBLIC_URL) is not set, in the environment or in the root .env.\n' +
    'Set it to this hub\'s public URL, e.g. HUB_URL=https://hub.example.com make build-shortcuts'
  );
  process.exit(1);
}

const outDir = path.join(__dirname, '..', '..', 'static', 'shortcuts');
fs.mkdirSync(outDir, { recursive: true });

const names = ['hub-health', 'run-command', 'shutdown-node'];

for (const name of names) {
  const src = fs
    .readFileSync(path.join(__dirname, `${name}.scpl`), 'utf8')
    .replace(/%%HUB_URL%%/g, hubUrl.replace(/\/+$/, ''));
  const result = parse(src, { make: ['shortcutplist'] });
  fs.writeFileSync(path.join(outDir, `${name}.shortcut`), result.shortcutplist);
  console.log(`built ${name}.shortcut -> ${hubUrl}`);
}
