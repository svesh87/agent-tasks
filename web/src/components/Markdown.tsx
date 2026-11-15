import { createContext, memo, useContext, useMemo, type ComponentProps, type ReactNode } from 'react';
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { PlanItem } from '../api/types';
import { ItemBox, ItemMeta, type ItemChange } from './PlanItem';

interface HastNode {
  type: string;
  tagName?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
  position?: { start: { offset?: number; line: number }; end: { offset?: number } };
}

/**
 * Wraps text nodes in <span class="src" data-s data-e> with their markdown source range,
 * so selections and comment anchors can be mapped to body offsets (lib/anchorDom.ts).
 */
function rehypeSourceSpans() {
  const walk = (node: HastNode, inherited?: [number, number]) => {
    if (!node.children) return;
    const own: [number, number] | undefined =
      node.position?.start.offset !== undefined && node.position.end.offset !== undefined
        ? [node.position.start.offset, node.position.end.offset]
        : inherited;
    node.children = node.children.map((child) => {
      if (child.type === 'text') {
        if (!child.value || child.value === '\n') return child;
        const pos: [number, number] | undefined =
          child.position?.start.offset !== undefined && child.position.end.offset !== undefined
            ? [child.position.start.offset, child.position.end.offset]
            : own;
        if (!pos) return child;
        return {
          type: 'element',
          tagName: 'span',
          properties: { className: ['src'], dataS: pos[0], dataE: pos[1] },
          children: [child],
        };
      }
      walk(child, own);
      return child;
    });
  };
  return (tree: HastNode) => walk(tree);
}

interface Props {
  body: string;
  /** Plan items of this revision; task-list lines render their state from here. */
  items?: PlanItem[];
  /** Wrap text in source spans (needed for anchoring). */
  anchors?: boolean;
  /** Owner ticks and notes; given only on the latest revision. */
  onItem?: ItemChange;
  className?: string;
}

// Item state reaches the list items through context, so the `components` map stays the same
// object across renders and React keeps the document's DOM (and text selections) intact.
interface ItemCtx {
  byLine: Map<number | undefined, PlanItem>;
  lines: string[];
  onItem?: ItemChange;
}
const ItemsContext = createContext<ItemCtx>({ byLine: new Map(), lines: [] });

function TaskListItem({ node, className: cls, children, ...p }: ComponentProps<'li'> & ExtraProps) {
  const { byLine, lines, onItem } = useContext(ItemsContext);
  if (!cls?.includes('task-list-item')) return <li className={cls} {...p}>{children}</li>;
  const line = node?.position?.start.line;
  const item = line !== undefined ? byLine.get(line) : undefined;
  // Fall back to the box written in the source when no item state is known.
  const written = /^\s*(?:[-*+]|\d+[.)])\s+\[[xX]\]/.test(lines[(line ?? 1) - 1] ?? '');
  const done = item ? item.done : written;
  return (
    <li className={`task-item${done ? ' done' : ''}`} {...p} data-item={item?.id}>
      <ItemBox item={item} done={done} onChange={onItem} />
      <div className="task-body">
        {children}
        {item && <ItemMeta item={item} onChange={onItem} />}
      </div>
    </li>
  );
}

const components: Components = {
  a: ({ node: _n, ...p }) => <a {...p} target="_blank" rel="noreferrer noopener" />,
  table: ({ node: _n, ...p }) => (
    <div className="table-wrap">
      <table {...p} />
    </div>
  ),
  input: ({ node: _n, ...p }) => (p.type === 'checkbox' ? null : <input {...p} />),
  li: TaskListItem,
};
const plugins = [remarkGfm];
const anchorPlugins = [rehypeSourceSpans];

/** The markdown body itself; re-renders only when the text changes. */
const Body = memo(function Body({ body, anchors }: { body: string; anchors?: boolean }) {
  return (
    <ReactMarkdown remarkPlugins={plugins} rehypePlugins={anchors ? anchorPlugins : []} components={components}>
      {body}
    </ReactMarkdown>
  );
});

/** A full markdown document: GFM tables, task lists with item state, code blocks. */
export function Markdown({ body, items, anchors, onItem, className }: Props) {
  const ctx = useMemo<ItemCtx>(
    () => ({ byLine: new Map((items ?? []).map((it) => [it.line, it])), lines: body.split('\n'), onItem }),
    [items, body, onItem],
  );
  return (
    <div className={`md ${className ?? ''}`}>
      <ItemsContext.Provider value={ctx}>
        <Body body={body} anchors={anchors} />
      </ItemsContext.Provider>
    </div>
  );
}

/** Short markdown (comments, replies, journal, notes) without block chrome. */
export function InlineMarkdown({ text }: { text: string }): ReactNode {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        a: ({ node: _n, ...p }) => <a {...p} target="_blank" rel="noreferrer noopener" />,
        p: ({ node: _n, ...p }) => <span className="p" {...p} />,
      }}
    >
      {text}
    </ReactMarkdown>
  );
}

/** Comment and journal bodies: paragraphs and lists allowed, compact spacing. */
export function SmallMarkdown({ text }: { text: string }) {
  return (
    <div className="md md-small">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{ a: ({ node: _n, ...p }) => <a {...p} target="_blank" rel="noreferrer noopener" /> }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
