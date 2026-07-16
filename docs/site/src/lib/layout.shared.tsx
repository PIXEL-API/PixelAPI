import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

function Logo() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect width="24" height="24" rx="7" fill="url(#pixel-logo-gradient)" />
      <path
        d="M8.5 17V7.5h3.7a3.1 3.1 0 1 1 0 6.2H10"
        stroke="white"
        strokeWidth="1.9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <defs>
        <linearGradient id="pixel-logo-gradient" x1="0" y1="0" x2="24" y2="24" gradientUnits="userSpaceOnUse">
          <stop stopColor="#3355D8" />
          <stop offset="1" stopColor="#7C5CFF" />
        </linearGradient>
      </defs>
    </svg>
  );
}

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          <Logo />
          <span className="font-semibold">Pixel API</span>
        </>
      ),
      url: '/',
      transparentMode: 'top',
    },
    links: [
      {
        text: '控制台',
        url: 'https://ai-pixel.online',
        external: true,
      },
    ],
  };
}
