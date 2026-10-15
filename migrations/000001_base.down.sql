-- 000001_base.down.sql
-- Intencionalmente NÃO remove usuarios/atividades/postgis: podem pertencer ao
-- schema principal do ZoneX. Remova manualmente em ambientes descartáveis.
DROP INDEX IF EXISTS idx_atividades_usuario_ativas;
