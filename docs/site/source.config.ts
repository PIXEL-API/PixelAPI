import { defineConfig, defineDocs } from 'fumadocs-mdx/config';
import { pageSchema } from 'fumadocs-core/source/schema';
import { z } from 'zod';

export const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    // 默认 frontmatter schema 会剔除未声明的字段，API 参考页需要额外的 endpoint 键，
    // 用于在服务端构造「本页目录」并关闭 full 布局。
    schema: pageSchema.extend({
      endpoint: z.string().optional(),
    }),
  },
});

export default defineConfig();
