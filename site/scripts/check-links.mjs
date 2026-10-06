// Checks every internal link and asset reference in dist/ resolves to a file,
// and that #fragments exist on the target page. External links are not fetched.
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, resolve, dirname } from 'node:path';

const dist = resolve('dist');
const base = (process.env.SITE_BASE || '/').replace(/\/$/, '');
const pages = [];
(function walk(d) {
  for (const n of readdirSync(d)) {
    const p = join(d, n);
    statSync(p).isDirectory() ? walk(p) : n.endsWith('.html') && pages.push(p);
  }
})(dist);

const ids = new Map();
const idsOf = (file) => {
  if (!ids.has(file)) {
    ids.set(file, new Set([...readFileSync(file, 'utf8').matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])));
  }
  return ids.get(file);
};

let bad = 0;
let checked = 0;
for (const page of pages) {
  const html = readFileSync(page, 'utf8');
  for (const m of html.matchAll(/\s(?:href|src)="([^"]+)"/g)) {
    let ref = m[1];
    if (/^(https?:|mailto:|data:)/.test(ref)) continue;
    const [path, frag] = ref.split('#');
    let target = page;
    if (path) {
      let p = path.split('?')[0];
      if (base && p.startsWith(base + '/')) p = p.slice(base.length);
      target = p.startsWith('/') ? join(dist, p) : join(dirname(page), p);
      if (existsSync(target) && statSync(target).isDirectory()) target = join(target, 'index.html');
    }
    checked++;
    if (!existsSync(target)) {
      console.error(`BROKEN  ${page.slice(dist.length)}  ->  ${ref}`);
      bad++;
    } else if (frag && target.endsWith('.html') && !idsOf(target).has(frag)) {
      console.error(`NO ANCHOR  ${page.slice(dist.length)}  ->  ${ref}`);
      bad++;
    }
  }
}
console.log(`${pages.length} pages, ${checked} internal references checked, ${bad} broken`);
process.exit(bad ? 1 : 0);
