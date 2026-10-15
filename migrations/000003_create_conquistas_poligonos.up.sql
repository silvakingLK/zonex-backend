-- 000003_create_conquistas_poligonos.up.sql
-- Tabela para registro histórico e auditoria de conquistas livres de território por polígono (Paper.io Mode).

CREATE TABLE IF NOT EXISTS conquistas_poligonos (
    id               SERIAL PRIMARY KEY,
    usuario_id       INTEGER NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    atividade_id     INTEGER REFERENCES atividades(id) ON DELETE SET NULL,
    area_m2          DOUBLE PRECISION NOT NULL,
    distancia_metros REAL NOT NULL DEFAULT 0,
    pontos           JSONB NOT NULL,
    criado_em        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_conquistas_poligonos_usuario
    ON conquistas_poligonos (usuario_id, criado_em DESC);
