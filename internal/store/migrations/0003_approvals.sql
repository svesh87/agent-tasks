-- The owner's approval of a document revision (CONTRACT.md v0.7). An approval
-- counts while its revision is the document's latest and it is not revoked;
-- rows are kept as history.
CREATE TABLE approvals (
    id          INTEGER PRIMARY KEY,
    revision_id INTEGER NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    revoked_at  TEXT
);
CREATE INDEX approvals_revision ON approvals (revision_id);
