-- View projection tables for materialized views

-- Projection name enumeration (deduplicates projection names, scoped to stream)
CREATE TABLE view_projection_names (
    id        BIGSERIAL PRIMARY KEY,
    stream_id BIGINT NOT NULL REFERENCES events_stream(id),
    name      TEXT NOT NULL,
    CONSTRAINT view_projection_names_stream_id_name_unique UNIQUE (stream_id, name)
);

-- Entity kind enumeration (deduplicates kind names)
CREATE TABLE view_projection_kinds (
    id   BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL UNIQUE
);

-- Single-key projected entity state (the common case)
CREATE TABLE view_projection_entries_single (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key           TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key)
);

-- Composite-key projected entity state (two-part keys)
CREATE TABLE view_projection_entries_composite (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key1          TEXT NOT NULL,
    key2          TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key1, key2)
);

-- Indexes for fast lookup by projection + kind
CREATE INDEX idx_vpes_projection_kind ON view_projection_entries_single(projection_id, kind_id);
CREATE INDEX idx_vpec_projection_kind ON view_projection_entries_composite(projection_id, kind_id);
