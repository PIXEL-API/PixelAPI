import type { ReactNode } from 'react';
import { ImageZoom } from 'fumadocs-ui/components/image-zoom';

type ScreenshotProps = {
  alt: string;
  children?: ReactNode;
  src: string;
};

export function Screenshot({ alt, children, src }: ScreenshotProps) {
  return (
    <figure className="docs-visual">
      <ImageZoom src={src} alt={alt}>
        {/* 截图尺寸不固定，跳过 next/image 的固定宽高要求，缩放大图仍走 ImageZoom */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={src} alt={alt} loading="lazy" />
      </ImageZoom>
      {children ? <figcaption>{children}</figcaption> : null}
    </figure>
  );
}
