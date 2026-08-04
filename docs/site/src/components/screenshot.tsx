import type { ReactNode } from 'react';
import { ImageZoom } from 'fumadocs-ui/components/image-zoom';
import { screenshotSizes } from './screenshot-manifest';

type ScreenshotProps = {
  alt: string;
  children?: ReactNode;
  src: string;
};

export function Screenshot({ alt, children, src }: ScreenshotProps) {
  const size = screenshotSizes[src];

  // 尺寸来自 pnpm images 生成的 manifest。缺了就说明截图没压过，
  // 与其静默丢掉 width/height 让页面加载时抖一下，不如直接让构建报错。
  if (!size) {
    throw new Error(
      `截图 ${src} 不在 screenshot-manifest 里。把原图放进 assets/screenshots/ 后运行 pnpm images。`,
    );
  }

  return (
    <figure className="docs-visual">
      <ImageZoom src={src} alt={alt}>
        {/* 截图已在构建前压成定宽 WebP，用原生 img 即可，不需要 next/image 的运行时优化 */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={src}
          alt={alt}
          width={size.width}
          height={size.height}
          loading="lazy"
          decoding="async"
        />
      </ImageZoom>
      {children ? <figcaption>{children}</figcaption> : null}
    </figure>
  );
}
