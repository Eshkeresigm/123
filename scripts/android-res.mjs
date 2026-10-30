// Installs the PluginSound launcher icons into the generated Capacitor Android project and makes the
// adaptive icon use them (the template ships its own vector foreground/background, which we remove).
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const from = path.join(root, 'resources', 'android', 'res');
const to = path.join(root, 'android', 'app', 'src', 'main', 'res');
if (!fs.existsSync(to)) { console.error('android project not found: run "cap add android" first'); process.exit(1); }

fs.cpSync(from, to, { recursive: true, force: true });

for (const f of ['drawable/ic_launcher_background.xml', 'drawable-v24/ic_launcher_foreground.xml', 'drawable-anydpi-v24/ic_launcher_foreground.xml']) {
  fs.rmSync(path.join(to, f), { force: true });
}
const adaptive = `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@mipmap/ic_launcher_foreground"/>
</adaptive-icon>
`;
const dir = path.join(to, 'mipmap-anydpi-v26');
fs.mkdirSync(dir, { recursive: true });
fs.writeFileSync(path.join(dir, 'ic_launcher.xml'), adaptive);
fs.writeFileSync(path.join(dir, 'ic_launcher_round.xml'), adaptive);
console.log('launcher icons installed into', to);
