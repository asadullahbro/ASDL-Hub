// Compiles the .scpl sources in this directory into .shortcut files served
// from static/shortcuts/. Re-run this (npm install && npm run build) after
// editing any .scpl file, e.g. if the hub domain or an API path changes.
const fs = require('fs');
const path = require('path');
const { parse } = require('scpl');

const outDir = path.join(__dirname, '..', '..', 'static', 'shortcuts');
fs.mkdirSync(outDir, { recursive: true });

const names = ['hub-health', 'run-command', 'shutdown-node'];

for (const name of names) {
  const src = fs.readFileSync(path.join(__dirname, `${name}.scpl`), 'utf8');
  const result = parse(src, { make: ['shortcutplist'] });
  fs.writeFileSync(path.join(outDir, `${name}.shortcut`), result.shortcutplist);
  console.log(`built ${name}.shortcut`);
}
