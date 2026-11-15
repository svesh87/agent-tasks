-- Initial schema. Timestamps are RFC 3339 UTC strings.

CREATE TABLE tasks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT, -- T-<id>; AUTOINCREMENT never reuses ids
    title         TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL CHECK (status IN ('active', 'waiting', 'later', 'archived')),
    waiting_on    TEXT NOT NULL DEFAULT '',
    closed_reason TEXT NOT NULL DEFAULT '' CHECK (closed_reason IN ('', 'done', 'cancelled')),
    archived_at   TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX tasks_status_updated ON tasks (status, updated_at);

CREATE TABLE task_tags (
    task_id INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (task_id, tag)
);
CREATE INDEX task_tags_tag ON task_tags (tag);

CREATE TABLE documents (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('plan', 'research', 'review', 'notes', 'other')),
    title      TEXT NOT NULL,
    next_item  INTEGER NOT NULL DEFAULT 1, -- next plan item number (i<n>)
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (task_id, slug)
);

CREATE TABLE revisions (
    id          INTEGER PRIMARY KEY,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    n           INTEGER NOT NULL,
    body        TEXT NOT NULL,
    author      TEXT NOT NULL CHECK (author IN ('agent', 'owner', 'import')),
    message     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    UNIQUE (document_id, n)
);

-- A plan item keeps its id, state and notes across revisions.
CREATE TABLE plan_items (
    id          INTEGER PRIMARY KEY,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    key         TEXT NOT NULL, -- i1, i2, ...
    done        INTEGER NOT NULL DEFAULT 0,
    done_at     TEXT,
    done_by     TEXT NOT NULL DEFAULT '' CHECK (done_by IN ('', 'agent', 'owner')), -- who ticked/unticked last
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (document_id, key)
);

-- Where an item appears in each revision (history; the current view is the
-- latest revision's rows).
CREATE TABLE plan_item_revisions (
    item_id     INTEGER NOT NULL REFERENCES plan_items (id) ON DELETE CASCADE,
    revision_id INTEGER NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL, -- 0-based index among the revision's items
    line        INTEGER NOT NULL, -- 1-based line in the body
    text        TEXT NOT NULL,
    PRIMARY KEY (revision_id, item_id)
);
CREATE INDEX plan_item_revisions_item ON plan_item_revisions (item_id);

CREATE TABLE plan_item_notes (
    id         INTEGER PRIMARY KEY,
    item_id    INTEGER NOT NULL REFERENCES plan_items (id) ON DELETE CASCADE,
    author     TEXT NOT NULL CHECK (author IN ('agent', 'owner')),
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX plan_item_notes_item ON plan_item_notes (item_id);

CREATE TABLE review_rounds (
    id           INTEGER PRIMARY KEY,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    n            INTEGER NOT NULL,
    revision_id  INTEGER NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    state        TEXT NOT NULL CHECK (state IN ('draft', 'submitted', 'addressed')),
    summary      TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '', -- agent's note when addressed
    created_at   TEXT NOT NULL,
    submitted_at TEXT,
    addressed_at TEXT,
    addressed_revision INTEGER NOT NULL DEFAULT 0, -- latest revision n when addressed
    UNIQUE (document_id, n)
);
CREATE INDEX review_rounds_state ON review_rounds (state);

CREATE TABLE comments (
    id            INTEGER PRIMARY KEY,
    round_id      INTEGER NOT NULL REFERENCES review_rounds (id) ON DELETE CASCADE,
    body          TEXT NOT NULL,
    state         TEXT NOT NULL CHECK (state IN ('open', 'resolved', 'outdated')),
    -- anchor on the reviewed revision; quote '' means a general comment
    quote         TEXT NOT NULL DEFAULT '',
    prefix        TEXT NOT NULL DEFAULT '',
    suffix        TEXT NOT NULL DEFAULT '',
    start_off     INTEGER NOT NULL DEFAULT 0,
    end_off       INTEGER NOT NULL DEFAULT 0,
    -- where the anchor sits in the latest revision it was found in
    cur_revision_id INTEGER REFERENCES revisions (id) ON DELETE SET NULL,
    cur_quote     TEXT NOT NULL DEFAULT '',
    cur_prefix    TEXT NOT NULL DEFAULT '',
    cur_suffix    TEXT NOT NULL DEFAULT '',
    cur_start     INTEGER NOT NULL DEFAULT 0,
    cur_end       INTEGER NOT NULL DEFAULT 0,
    outdated_revision_id INTEGER REFERENCES revisions (id) ON DELETE SET NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX comments_round ON comments (round_id);

CREATE TABLE comment_replies (
    id         INTEGER PRIMARY KEY,
    comment_id INTEGER NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
    author     TEXT NOT NULL CHECK (author IN ('agent', 'owner')),
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX comment_replies_comment ON comment_replies (comment_id);

CREATE TABLE journal (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('context', 'log', 'decision')),
    body       TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX journal_task ON journal (task_id, created_at);

-- Full-text index. The application stores stems (see internal/textproc) in
-- `stems`; `raw` keeps the original text for snippets. `src` names the
-- source: task, doc:<slug>, journal:<id>, comment:<id>.
CREATE VIRTUAL TABLE search USING fts5 (
    task_id UNINDEXED,
    src UNINDEXED,
    raw UNINDEXED,
    stems,
    tokenize = 'unicode61 remove_diacritics 0'
);
