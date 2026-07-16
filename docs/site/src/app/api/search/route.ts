import { source } from '@/lib/source';
import { createSearchAPI } from 'fumadocs-core/search/server';
import { createTokenizer } from '@orama/tokenizers/mandarin';

export const { GET } = createSearchAPI('advanced', {
  components: {
    tokenizer: createTokenizer(),
  },
  search: {
    threshold: 0,
    tolerance: 0,
  },
  indexes: source.getPages().map((page) => ({
    title: page.data.title ?? page.url,
    description: page.data.description ?? '',
    url: page.url,
    id: page.url,
    structuredData: page.data.structuredData,
  })),
});
