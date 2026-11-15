// A small inline icon set (stroke icons on a 16px grid), so the page needs no icon font.
const paths = {
  search: 'M7 12.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11zM11 11l3.5 3.5',
  sun: 'M8 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM8 1v1.5M8 13.5V15M1 8h1.5M13.5 8H15M3 3l1 1M12 12l1 1M3 13l1-1M12 4l1-1',
  moon: 'M13.5 9.5A5.5 5.5 0 0 1 6.5 2.5a5.5 5.5 0 1 0 7 7z',
  system: 'M2 3h12v8H2zM6 14h4M8 11v3',
  chevronDown: 'M4 6l4 4 4-4',
  chevronRight: 'M6 4l4 4-4 4',
  chevronLeft: 'M10 4L6 8l4 4',
  x: 'M4 4l8 8M12 4l-8 8',
  check: 'M3.5 8.5l3 3 6-7',
  comment: 'M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2z',
  history: 'M2.5 8a5.5 5.5 0 1 0 1.6-3.9M2.5 2.5v2.5H5M8 5v3l2 1.5',
  diff: 'M5 2v8M2 7l3 3 3-3M11 14V6M8 9l3-3 3 3',
  folder: 'M1.5 4V13h13V5.5H7.5L6 4z',
  copy: 'M5.5 5.5h8v8h-8zM10.5 5.5v-3h-8v8h3',
  reply: 'M6.5 4L2.5 8l4 4M2.5 8h7a4 4 0 0 1 4 4',
  doc: 'M3.5 1.5h6l3 3v10h-9zM9.5 1.5v3h3',
  globe: 'M8 14.5a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM1.5 8h13M8 1.5c2 2 2 11 0 13M8 1.5c-2 2-2 11 0 13',
  filter: 'M2 3h12l-4.5 5.5V13l-3 1.5v-6z',
  tag: 'M1.5 1.5h6l7 7-6 6-7-7zM5 5h.01',
  link: 'M6.5 9.5l3-3M7 4.5l1-1a2.8 2.8 0 0 1 4 4l-1 1M9 11.5l-1 1a2.8 2.8 0 0 1-4-4l1-1',
  repo: 'M3.5 13V2.5h9v9h-9M3.5 13a1.5 1.5 0 0 1 1.5-1.5h7.5M6 2.5v5l1.25-1 1.25 1v-5',
  plus: 'M8 3v10M3 8h10',
  arrowRight: 'M3 8h10M9 4l4 4-4 4',
  dot: 'M8 8h.01',
  edit: 'M10.5 2.5l3 3-8 8h-3v-3z',
  trash: 'M2.5 4h11M6 4V2.5h4V4M4 4l.7 9.5h6.6L12 4',
} as const;

export type IconName = keyof typeof paths;

export function Icon({ name, size = 16, className }: { name: IconName; size?: number; className?: string }) {
  return (
    <svg
      className={`icon ${className ?? ''}`}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
