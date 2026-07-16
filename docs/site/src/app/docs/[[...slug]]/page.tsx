import { notFound } from 'next/navigation';
import { DocsBody, DocsPage, DocsTitle } from 'fumadocs-ui/page';
import { source } from '@/lib/source';
import { getMDXComponents } from '@/mdx-components';
import { buildEndpointToc, endpoints, type EndpointId } from '@/components/model-api-data';

type PageProps = {
  params: Promise<{
    slug?: string[];
  }>;
};

type DocsModule = 'guide' | 'wallet' | 'rewards' | 'api' | 'operations';

function getDocsModule(slug?: string[]): DocsModule {
  const section = slug?.[0];

  if (section === 'wallet' || section === 'rewards' || section === 'api' || section === 'operations') {
    return section;
  }

  return 'guide';
}

function getDocsPageClassName(docsModule: DocsModule, endpointId?: EndpointId) {
  return ['docs-page-shell', `docs-module-${docsModule}`, endpointId ? 'model-api-doc-page' : undefined]
    .filter(Boolean)
    .join(' ');
}

export default async function Page(props: PageProps) {
  const params = await props.params;
  const page = source.getPage(params.slug);

  if (!page) {
    notFound();
  }

  const MDX = page.data.body;

  // API 参考页通过 frontmatter 的 endpoint 字段声明对应端点。
  // 这类页面自带右侧代码/响应面板，因此关闭 Fumadocs 右侧 TOC，把空间留给 API 控件。
  const rawEndpoint = (page.data as { endpoint?: string }).endpoint;
  const endpointId =
    rawEndpoint && rawEndpoint in endpoints ? (rawEndpoint as EndpointId) : undefined;
  const toc = endpointId ? buildEndpointToc(endpointId) : page.data.toc;
  const full = endpointId ? true : page.data.full;
  const docsModule = getDocsModule(params.slug);

  return (
    <DocsPage
      className={getDocsPageClassName(docsModule, endpointId)}
      toc={toc}
      full={full}
      tableOfContent={endpointId ? { enabled: false } : { style: 'clerk' }}
      tableOfContentPopover={endpointId ? { enabled: false } : undefined}
    >
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsBody>
        <MDX components={getMDXComponents()} />
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps) {
  const params = await props.params;
  const page = source.getPage(params.slug);

  if (!page) {
    notFound();
  }

  return {
    title: page.data.title,
    description: page.data.description,
  };
}
