type QqGroup = {
  /** 群序号，用于「PIXEL API QQ群（N群）」的 N */
  index: number;
  /** 群号 */
  number: string;
  /** 加群链接 */
  href: string;
  /** 是否已满。群状态变动只改这一个字段 */
  full: boolean;
};

/**
 * 加群入口列表。
 *
 * 群状态经常变，所以这里只维护一个数组，样式由组件统一给。
 * 以前这块是在 MDX 里手写 8 段重复 JSX（每段 20 行、含暗色类名），
 * 加一个群或改一个「已满」都要复制粘贴一整段。
 */
export function QqGroups({ groups }: { groups: QqGroup[] }) {
  return (
    <div className="not-prose mt-4 overflow-hidden rounded-lg border border-fd-border bg-fd-card shadow-sm">
      <div className="divide-y divide-fd-border">
        {groups.map((group) => (
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 p-4" key={group.number}>
            <span className="text-sm font-semibold text-fd-foreground">
              PIXEL API QQ群（{group.index}群）：
            </span>
            <a
              href={group.href}
              target="_blank"
              rel="noreferrer"
              className="inline-flex w-28 justify-center rounded-md border border-fd-border bg-fd-muted px-2.5 py-1 text-sm font-semibold text-fd-foreground transition-colors hover:border-fd-primary hover:text-fd-primary"
            >
              {group.number}
            </a>
            {group.full ? (
              <span className="rounded-full border border-red-200 bg-red-50 px-2.5 py-1 text-xs font-semibold text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300">
                已满
              </span>
            ) : (
              <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300">
                未满
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
