// Data model as the web UI sees it. Mirrors CONTRACT.md (v0.5) and the JSON API in
// docs/backend-design.md; field names are the API's (snake_case) so a REST client can pass
// JSON through. Where the UI groups data differently from the API (comments nested in
// rounds, diff computed from two bodies), the REST client does the reshaping.
//
// Offsets: the API counts anchor offsets in Unicode code points; the UI works in JS string
// indices (UTF-16). The REST client converts with lib/markdown.ts cpToUtf16/utf16ToCp.

export type TaskStatus = 'active' | 'waiting' | 'later' | 'archived';
export type ClosedReason = 'done' | 'cancelled';
export type DocKind = 'plan' | 'research' | 'review' | 'notes' | 'other';
export type Author = 'agent' | 'owner' | 'import';
export type RoundState = 'draft' | 'submitted' | 'addressed';
export type CommentState = 'open' | 'resolved' | 'outdated';
export type JournalKind = 'context' | 'log' | 'decision';
export type SortKey = 'updated' | 'created' | 'relevance';
/** Who ticked an item or wrote a note. */
export type By = 'owner' | 'agent';
export type PlaceKind = 'git' | 'path';
export type PlaceRole = 'primary' | 'touched';
export type TodoState = 'open' | 'done' | 'dropped' | 'in_task';

export interface PlaceLocation {
  machine: string;
  path: string;
  seen_at: string;
}

/** A repository (`git`, key `host/path`) or a directory without a remote (`path`, key `machine:/abs`). */
export interface Place {
  key: string;
  kind: PlaceKind;
  label: string;
  locations?: PlaceLocation[];
}

export interface TaskPlace extends Place {
  role: PlaceRole;
  note?: string;
}

export interface PlaceSummary extends Place {
  tasks: number;
  open_todos: number;
}

export interface Task {
  id: string;
  title: string;
  summary?: string;
  status: TaskStatus;
  waiting_on?: string;
  closed_reason?: ClosedReason;
  archived_at?: string;
  tags: string[];
  places: TaskPlace[];
  created_at: string;
  updated_at: string;
}

export interface Todo {
  id: string;
  text: string;
  note?: string;
  state: TodoState;
  /** Set when in_task. */
  task?: string;
  /** Provenance only: the task where the idea came up. Not a parent. */
  origin_task?: string;
  places: Place[];
  tags: string[];
  source?: string;
  created_at: string;
  updated_at: string;
  closed_at?: string;
}

export interface Revision {
  n: number;
  body: string;
  author: Author;
  message?: string;
  created_at: string;
}

export type RevisionMeta = Omit<Revision, 'body'>;

export interface ItemNote {
  body: string;
  /** API field `author`. */
  author: By;
  created_at: string;
}

export interface PlanItem {
  id: string;
  text: string;
  done: boolean;
  /** Who ticked it (absent for items ticked in the body or by older servers). */
  done_by?: By;
  done_at?: string;
  notes: ItemNote[];
  /** 1-based line of the item in the revision being shown. */
  line?: number;
}

export interface Anchor {
  quote: string;
  prefix: string;
  suffix: string;
  start: number;
  end: number;
}

export interface CurrentAnchor {
  revision: number;
  quote: string;
  start: number;
  end: number;
}

export interface Reply {
  id: number;
  author: 'agent' | 'owner';
  body: string;
  created_at: string;
}

export interface Comment {
  id: number;
  round: number;
  /** The reviewed revision; `anchor` is on it. */
  revision: number;
  /** Null for a general comment. */
  anchor: Anchor | null;
  /** Where the text sits in the newest revision it was found in. */
  current?: CurrentAnchor;
  /** Revision where the text was no longer found. */
  outdated_in?: number;
  body: string;
  state: CommentState;
  replies: Reply[];
  created_at: string;
}

export interface ReviewRound {
  id: number;
  n: number;
  /** Revision the round reviews. */
  revision: number;
  state: RoundState;
  summary?: string;
  created_at: string;
  submitted_at?: string;
  addressed_at?: string;
  addressed_message?: string;
  /** Revision written while addressing the round, if any (UI convenience; derivable). */
  addressed_in?: number;
  comments: Comment[];
}

export interface DocumentSummary {
  slug: string;
  kind: DocKind;
  title: string;
  latest: number;
  latest_author?: Author;
  updated_at: string;
  plan?: { done: number; total: number };
  open_comments: number;
  draft_round?: { n: number; comments: number };
  submitted_round?: { n: number; comments: number };
  /** The owner's approval of the latest revision. */
  approved?: Approval;
}

/** The owner's "go" on a revision; it counts while that revision is the latest. */
export interface Approval {
  revision: number;
  at: string;
}

export interface JournalEntry {
  id: number;
  kind: JournalKind;
  body: string;
  source?: string;
  created_at: string;
}

export interface TaskListItem {
  task: Task;
  plan?: { done: number; total: number };
  documents: number;
  submitted_rounds: number;
  draft_rounds: number;
  /** What the search matched, best first (absent without a query). */
  hits?: SearchHit[];
}

export type HitKind = 'task' | 'document' | 'comment' | 'round' | 'journal' | 'todo';

/** One search match: the snippet (matches wrapped in [[ ]]) and the app path to jump to. */
export interface SearchHit {
  kind: HitKind;
  /** Document title, comment author… — shown before the snippet. */
  label?: string;
  text: string;
  /** App path of the matched object, e.g. /t/T-1/d/plan/r/2?hl=…, /t/T-1/d/plan?c=5, /todos/D-3. */
  path: string;
}

export interface TaskListQuery {
  q?: string;
  status: TaskStatus[];
  tags: string[];
  /** Place key, or 'none' for tasks without places. */
  place?: string;
  sort: SortKey;
}

export interface TaskListResult {
  items: TaskListItem[];
  status_counts: Record<TaskStatus, number>;
  tag_counts: { tag: string; count: number }[];
}

/** A round of the task's documents that is a draft or waits for the agent. */
export interface PendingRound {
  id: number;
  slug: string;
  doc_title: string;
  n: number;
  revision: number;
  state: RoundState;
  comments: number;
}

export interface TaskDetail {
  task: Task;
  url: string;
  workspace: string;
  documents: DocumentSummary[];
  plan?: { slug: string; done: number; total: number; items: PlanItem[] };
  journal: JournalEntry[];
  rounds: PendingRound[];
  /** Todos taken into this task. */
  todos: Todo[];
}

export interface DocumentView {
  task: Task;
  slug: string;
  kind: DocKind;
  title: string;
  latest: number;
  revision: Revision;
  revisions: RevisionMeta[];
  items: PlanItem[];
  /** All rounds of the document, newest first. */
  rounds: ReviewRound[];
  /** Approval of the latest revision, if any. */
  approved?: Approval;
  /** Revisions approved at some point (history). */
  approved_revisions: number[];
}

export interface DiffView {
  task: Task;
  slug: string;
  title: string;
  latest: number;
  revisions: RevisionMeta[];
  from: Revision;
  to: Revision;
  /** Rounds made on revisions from..to-1: what the change was answering. */
  rounds: ReviewRound[];
}

export interface TaskPatch {
  title?: string;
  summary?: string;
  status?: TaskStatus;
  waiting_on?: string;
  closed_reason?: ClosedReason;
  tags_add?: string[];
  tags_remove?: string[];
  places_add?: { place: string; role?: PlaceRole; note?: string }[];
  places_remove?: string[];
}

export interface NewTask {
  title: string;
  summary?: string;
  status?: TaskStatus;
  tags?: string[];
  from_todos?: string[];
}

export interface NewTodo {
  text: string;
  note?: string;
  places?: string[];
  tags?: string[];
}

export interface TodoQuery {
  q?: string;
  /** Place key, or 'none'. */
  place?: string;
  state: TodoState[];
  tags: string[];
}

export interface TodoListResult {
  total: number;
  todos: Todo[];
  /** UI facets (the REST client derives them from /api/places and /api/tags). */
  state_counts: Record<TodoState, number>;
  tag_counts: { tag: string; count: number }[];
}

export interface TodoPatch {
  text?: string;
  note?: string;
  state?: 'open' | 'done' | 'dropped';
  places_add?: string[];
  places_remove?: string[];
  tags_add?: string[];
  tags_remove?: string[];
}

/** Everything the UI needs from the server. The fake and the REST client implement it. */
export interface Api {
  listTasks(q: TaskListQuery): Promise<TaskListResult>;
  getTask(id: string, machine?: string): Promise<TaskDetail>;
  createTask(t: NewTask): Promise<Task>;
  updateTask(id: string, patch: TaskPatch): Promise<Task>;
  listPlaces(): Promise<PlaceSummary[]>;
  /** Tags of tasks and todos with use counts, most used first. */
  listTags(): Promise<{ tag: string; count: number }[]>;
  listTodos(q: TodoQuery): Promise<TodoListResult>;
  getTodo(id: string): Promise<Todo>;
  updateTodo(id: string, patch: TodoPatch): Promise<Todo>;
  createTodo(t: NewTodo): Promise<Todo>;
  /** Same status change for several tasks (archiving needs closed_reason). */
  bulkUpdateTasks(ids: string[], patch: { status: TaskStatus; waiting_on?: string; closed_reason?: ClosedReason }): Promise<void>;
  /** Close or reopen several todos. */
  bulkUpdateTodos(ids: string[], state: 'open' | 'done' | 'dropped'): Promise<void>;
  /**
   * Saves a new revision written by the owner. `base` is the revision the edit started from;
   * if the document has moved on, the call fails with ConflictError and changes nothing.
   */
  writeDocument(task: string, slug: string, body: string, base: number, message?: string): Promise<{ n: number }>;
  /** A new revision by the owner with the body of revision `to`; ConflictError as for writeDocument. */
  revertDocument(task: string, slug: string, to: number, base: number): Promise<{ n: number }>;
  /**
   * The owner approves revision `revision` (must be the latest, else ConflictError). With
   * `submitDraft`, a draft round with comments is submitted along with it.
   */
  approveDocument(task: string, slug: string, revision: number, submitDraft: boolean): Promise<void>;
  /** Withdraws the approval of the latest revision. */
  revokeApproval(task: string, slug: string): Promise<void>;
  /** Tick/untick a plan item and/or add a note, as the owner. */
  setItem(task: string, slug: string, item: string, change: { done?: boolean; note?: string }): Promise<void>;
  getDocument(task: string, slug: string, revision?: number): Promise<DocumentView>;
  getDiff(task: string, slug: string, from: number, to: number): Promise<DiffView>;
  /** Adds a comment to the document's draft round (created if missing) on the latest revision. */
  addDraftComment(task: string, slug: string, c: { anchor: Anchor | null; body: string }): Promise<Comment>;
  updateDraftComment(commentId: number, body: string): Promise<void>;
  deleteDraftComment(commentId: number): Promise<void>;
  setRoundSummary(roundId: number, summary: string): Promise<void>;
  submitRound(roundId: number): Promise<void>;
  discardRound(roundId: number): Promise<void>;
  reply(commentId: number, body: string): Promise<void>;
  setCommentState(commentId: number, state: 'open' | 'resolved'): Promise<void>;
}

/** The comment's anchor on revision n, if it is shown there (API rule: revision or current.revision). */
/** The document changed since the edit started; `latest` is its newest revision now. */
export class ConflictError extends Error {
  constructor(
    message: string,
    public latest: number,
  ) {
    super(message);
  }
}

export function anchorOn(c: Comment, n: number): { start: number; end: number; quote: string } | null {
  if (!c.anchor) return null;
  if (c.revision === n) return c.anchor;
  if (c.current && c.current.revision === n) return c.current;
  return null;
}
