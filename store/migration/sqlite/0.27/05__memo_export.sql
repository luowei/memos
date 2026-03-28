CREATE TABLE memo_export (
  memo_id INTEGER NOT NULL PRIMARY KEY,
  export_ts BIGINT NOT NULL,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  FOREIGN KEY (memo_id) REFERENCES memo(id) ON DELETE CASCADE
);
