import { access, readFile, readdir } from 'node:fs/promises';
import { extname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteRoot = fileURLToPath(new URL('..', import.meta.url));
const docsRoot = join(siteRoot, 'content', 'docs');

const requiredRoutes = [
  '/',
  '/api/search',
  '/docs',
  '/docs/api',
  '/docs/api/chat-completions',
  '/docs/api/responses',
  '/docs/normal-first-message',
  '/docs/normal-troubleshooting',
  '/docs/operations/faq',
];

const redirects = new Map([
  ['/docs/glossary', '/docs/concepts'],
  ['/docs/accounts', '/docs/normal-account-mode'],
  ['/docs/usage', '/docs/normal-check-usage'],
  ['/docs/client-setup', '/docs/normal-client-setup'],
  ['/docs/owner-income', '/docs/owner-check-income'],
  ['/docs/operations/troubleshooting', '/docs/operations/faq'],
]);

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await walk(path)));
    } else if (entry.isFile()) {
      files.push(path);
    }
  }

  return files;
}

function docsRouteFromFile(path) {
  const parts = relative(docsRoot, path)
    .split(sep)
    .filter((part) => !/^\(.+\)$/.test(part));
  const filename = parts.pop();
  const name = filename.slice(0, -extname(filename).length);

  if (name !== 'index') {
    parts.push(name);
  }

  return parts.length === 0 ? '/docs' : `/docs/${parts.join('/')}`;
}

function internalPath(rawTarget) {
  const target = rawTarget.trim().replace(/^['"]|['"]$/g, '');
  if (!target.startsWith('/') || target.startsWith('//')) {
    return undefined;
  }

  return target.split(/[?#]/, 1)[0].replace(/\/$/, '') || '/';
}

function collectInternalLinks(content) {
  const links = [];
  const patterns = [
    /\[[^\]]*\]\((\/[^\s)]+)(?:\s+['"][^'"]*['"])?\)/g,
    /\bhref\s*=\s*["'](\/[^"']*)["']/g,
    /\b(?:href|url)\s*:\s*["'](\/[^"']*)["']/g,
  ];

  for (const pattern of patterns) {
    for (const match of content.matchAll(pattern)) {
      const path = internalPath(match[1]);
      if (path) {
        links.push(path);
      }
    }
  }

  return links;
}

const docsFiles = (await walk(docsRoot)).filter((path) => extname(path) === '.mdx');
const sourceFiles = [
  ...docsFiles,
  ...(await walk(join(siteRoot, 'src'))).filter((path) => ['.ts', '.tsx'].includes(extname(path))),
];

const routes = new Set(['/', ...docsFiles.map(docsRouteFromFile)]);
const failures = [];

try {
  await access(join(siteRoot, 'src', 'app', 'api', 'search', 'route.ts'));
  routes.add('/api/search');
} catch {
  failures.push('required route file is missing: src/app/api/search/route.ts');
}

for (const route of [...requiredRoutes, ...redirects.values()]) {
  if (!routes.has(route)) {
    failures.push(`required route is missing: ${route}`);
  }
}

const nextConfig = await readFile(join(siteRoot, 'next.config.ts'), 'utf8');
const configuredRedirects = new Map(
  [...nextConfig.matchAll(
    /\{\s*source:\s*['"]([^'"]+)['"],\s*destination:\s*['"]([^'"]+)['"],\s*permanent:\s*true\s*,?\s*\}/g,
  )].map((match) => [match[1], match[2]]),
);
for (const [source, destination] of redirects) {
  if (configuredRedirects.get(source) !== destination) {
    failures.push(`redirect is missing or has the wrong destination: ${source} -> ${destination}`);
  }
}

for (const file of sourceFiles) {
  const content = await readFile(file, 'utf8');
  for (const link of collectInternalLinks(content)) {
    if (!routes.has(link)) {
      failures.push(`${relative(siteRoot, file)} links to missing route ${link}`);
    }
  }
}

if (failures.length > 0) {
  console.error('Documentation link validation failed:');
  for (const failure of [...new Set(failures)].sort()) {
    console.error(`- ${failure}`);
  }
  process.exitCode = 1;
} else {
  console.log(`Validated ${routes.size} documentation routes across ${sourceFiles.length} source files.`);
}
