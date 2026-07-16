import { docs } from '@/.source';
import { loader } from 'fumadocs-core/source';
import { createElement } from 'react';
import { BookIcon, CodeIcon, GiftIcon, ShieldIcon, WalletIcon } from '@/components/icons';

const icons = {
  Book: BookIcon,
  Code: CodeIcon,
  Shield: ShieldIcon,
  Wallet: WalletIcon,
  Gift: GiftIcon,
} as const;

export const source = loader({
  baseUrl: '/docs',
  icon(icon) {
    if (icon && icon in icons) {
      return createElement(icons[icon as keyof typeof icons]);
    }
  },
  source: docs.toFumadocsSource(),
});
