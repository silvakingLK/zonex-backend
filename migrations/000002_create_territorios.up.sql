-- 000002_create_territorios.up.sql
-- Células H3 dominadas. Células ausentes desta tabela são NEUTRAS.

CREATE TABLE IF NOT EXISTS territorios (
    -- ID H3 em hexadecimal (15 chars na prática; 32 dá folga).
    -- A PK já cria um índice B-tree único: consultas por hex_id
    -- (= e = ANY(array)) usam esse índice; um índice extra seria redundante.
    hex_id        VARCHAR(32) PRIMARY KEY,

    -- NULL => célula neutra (ex.: conta do dono excluída).
    dono_id       INTEGER     REFERENCES usuarios(id) ON DELETE SET NULL,
    dominado_em   TIMESTAMPTZ NOT NULL DEFAULT now(),
    pontos_defesa INTEGER     NOT NULL DEFAULT 1 CHECK (pontos_defesa >= 1),

    -- Auditoria / antifraude: última atividade que agiu sobre a célula.
    atividade_id  INTEGER     REFERENCES atividades(id) ON DELETE SET NULL,

    -- Centróide da célula (PostGIS) para consultas espaciais, heatmaps e
    -- rankings por região sem depender da biblioteca H3 no banco.
    centro        geometry(Point, 4326) NOT NULL,

    atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Meus territórios" / ranking por dono, já ordenado pelos mais recentes.
-- Também atende WHERE dono_id = ? sozinho (prefixo do índice composto).
CREATE INDEX IF NOT EXISTS idx_territorios_dono_dominado
    ON territorios (dono_id, dominado_em DESC);

-- Consultas espaciais (ST_DWithin, ST_Intersects com bounding box, etc.).
CREATE INDEX IF NOT EXISTS idx_territorios_centro_gist
    ON territorios USING GIST (centro);
