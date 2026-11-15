-- Places (repositories and directories), where they live on each machine,
-- task places, and todos.

CREATE TABLE places (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('git', 'path')),
    key        TEXT NOT NULL UNIQUE, -- git: host/path; path: <machine>:<absolute path>
    label      TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- Learned from the session hook and place_here: where a place lives on a machine.
CREATE TABLE place_locations (
    place_id INTEGER NOT NULL REFERENCES places (id) ON DELETE CASCADE,
    machine  TEXT NOT NULL,
    path     TEXT NOT NULL,
    seen_at  TEXT NOT NULL,
    PRIMARY KEY (place_id, machine, path)
);
CREATE INDEX place_locations_machine ON place_locations (machine, path);

CREATE TABLE task_places (
    task_id  INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    place_id INTEGER NOT NULL REFERENCES places (id) ON DELETE CASCADE,
    role     TEXT NOT NULL CHECK (role IN ('primary', 'touched')),
    note     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, place_id)
);
CREATE INDEX task_places_place ON task_places (place_id);

-- Todos stand alone: deleting a task never deletes a todo, it only clears
-- the reference.
CREATE TABLE todos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT, -- D-<id>, never reused
    text           TEXT NOT NULL,
    note           TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL CHECK (state IN ('open', 'done', 'dropped', 'in_task')),
    origin_task_id INTEGER REFERENCES tasks (id) ON DELETE SET NULL,
    task_id        INTEGER REFERENCES tasks (id) ON DELETE SET NULL, -- set when in_task
    source         TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    closed_at      TEXT
);
CREATE INDEX todos_state ON todos (state, created_at);

CREATE TABLE todo_tags (
    todo_id INTEGER NOT NULL REFERENCES todos (id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (todo_id, tag)
);

CREATE TABLE todo_places (
    todo_id  INTEGER NOT NULL REFERENCES todos (id) ON DELETE CASCADE,
    place_id INTEGER NOT NULL REFERENCES places (id) ON DELETE CASCADE,
    PRIMARY KEY (todo_id, place_id)
);
CREATE INDEX todo_places_place ON todo_places (place_id);

-- Full-text index of todos (text + note), stems as in `search`.
CREATE VIRTUAL TABLE todo_search USING fts5 (
    todo_id UNINDEXED,
    raw UNINDEXED,
    stems,
    tokenize = 'unicode61 remove_diacritics 0'
);
