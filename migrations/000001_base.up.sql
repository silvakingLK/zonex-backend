-- 000001_base.up.sql
-- Pré-requisitos mínimos para o motor de territórios.
-- Se o banco do ZoneX já possui `usuarios` e `atividades`, esta migration é
-- inofensiva (IF NOT EXISTS), mas confira se as colunas usadas existem:
--   usuarios.id, atividades.id, atividades.usuario_id, atividades.status

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS usuarios (
    id         SERIAL PRIMARY KEY,
    nome       VARCHAR(120) NOT NULL,
    criado_em  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS atividades (
    id            SERIAL PRIMARY KEY,
    usuario_id    INTEGER     NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'em_andamento'
                  CHECK (status IN ('em_andamento', 'pausada', 'finalizada', 'cancelada')),
    iniciada_em   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalizada_em TIMESTAMPTZ
);

-- Acelera a validação "usuário está em uma atividade ativa".
CREATE INDEX IF NOT EXISTS idx_atividades_usuario_ativas
    ON atividades (usuario_id, id)
    WHERE status = 'em_andamento';
