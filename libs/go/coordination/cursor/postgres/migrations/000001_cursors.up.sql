CREATE TABLE IF NOT EXISTS coordination_cursors (
  namespace TEXT NOT NULL,
  name TEXT NOT NULL,
  sequence BIGINT NOT NULL CHECK (sequence >= 0),
  opaque BYTEA NOT NULL DEFAULT ''::bytea,
  fence BIGINT NOT NULL CHECK (fence > 0),
  version BIGINT NOT NULL CHECK (version > 0),
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(namespace,name)
);
