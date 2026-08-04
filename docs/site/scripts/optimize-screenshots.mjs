/**
 * 把 assets/screenshots 下的原始截图压成 public/images/guide 里的 WebP，
 * 并生成 src/components/screenshot-manifest.ts。
 *
 * 加新截图的流程：
 *   1. 原始 PNG 放进 assets/screenshots/（这个目录不对外提供，只是源文件仓库）
 *   2. pnpm images
 *   3. MDX 里用 <Screenshot src="/images/guide/<文件名>.webp" ...>
 *
 * 尺寸写进 manifest 是为了让 <img> 带上 width/height，
 * 图片加载完不会再撑开一次布局（CLS）。
 */
import { readdir, writeFile, mkdir, stat } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import sharp from 'sharp';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceDir = path.join(root, 'assets/screenshots');
const outputDir = path.join(root, 'public/images/guide');
const manifestPath = path.join(root, 'src/components/screenshot-manifest.ts');

// 正文最宽约 750px，2x 屏幕够用；再大只是浪费流量。
const MAX_WIDTH = 1600;
const QUALITY = 82;

const files = (await readdir(sourceDir))
  .filter((name) => /\.(png|jpe?g)$/i.test(name))
  .sort();

if (files.length === 0) {
  console.error(`没有在 ${sourceDir} 找到截图源文件`);
  process.exit(1);
}

await mkdir(outputDir, { recursive: true });

const entries = [];
let sourceBytes = 0;
let outputBytes = 0;

for (const name of files) {
  const base = name.replace(/\.(png|jpe?g)$/i, '');
  const target = path.join(outputDir, `${base}.webp`);

  const sourcePath = path.join(sourceDir, name);
  const pipeline = sharp(sourcePath);
  const { width: srcWidth } = await pipeline.metadata();
  const srcSize = (await stat(sourcePath)).size;

  const info = await pipeline
    .resize({ width: Math.min(srcWidth ?? MAX_WIDTH, MAX_WIDTH), withoutEnlargement: true })
    .webp({ quality: QUALITY })
    .toFile(target);

  sourceBytes += srcSize;
  outputBytes += info.size;
  entries.push({ src: `/images/guide/${base}.webp`, width: info.width, height: info.height });

  const kb = (n) => `${Math.round(n / 1024)}KB`;
  console.log(`${name} → ${base}.webp  ${info.width}x${info.height}  ${kb(srcSize)} → ${kb(info.size)}`);
}

const body = entries
  .map((e) => `  '${e.src}': { width: ${e.width}, height: ${e.height} },`)
  .join('\n');

await writeFile(
  manifestPath,
  `// 由 pnpm images 生成，不要手改。\n` +
    `// 截图源文件在 assets/screenshots/，压缩产物在 public/images/guide/。\n\n` +
    `export const screenshotSizes: Record<string, { width: number; height: number }> = {\n` +
    `${body}\n};\n`,
  'utf8',
);

const mb = (n) => `${(n / 1024 / 1024).toFixed(2)}MB`;
console.log(`\n共 ${entries.length} 张：${mb(sourceBytes)} → ${mb(outputBytes)}`);
console.log(`manifest 已写入 ${path.relative(root, manifestPath)}`);
