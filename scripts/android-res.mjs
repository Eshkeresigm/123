// Copies the pre-rendered launcher icons into the generated Capacitor Android project.
import fs from 'node:fs';
import path from 'node:path';
const root = path.resolve(path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')), '..');
const from = path.join(root, 'resources', 'android', 'res');
const to = path.join(root, 'android', 'app', 'src', 'main', 'res');
if (!fs.existsSync(to)) { console.error('android project not found: run "cap add android" first'); process.exit(1); }
fs.cpSync(from, to, { recursive: true, force: true });
console.log('launcher icons copied to', to);
