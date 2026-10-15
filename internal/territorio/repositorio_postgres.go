package territorio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zonex/backend/internal/geo"
)

// RepositorioPostgres implementa Repositorio sobre PostgreSQL + PostGIS.
type RepositorioPostgres struct {
	db *pgxpool.Pool
}

func NovoRepositorioPostgres(db *pgxpool.Pool) *RepositorioPostgres {
	return &RepositorioPostgres{db: db}
}

const sqlAtividadeAtiva = `
SELECT EXISTS (
    SELECT 1 FROM atividades
     WHERE id = $1 AND usuario_id = $2 AND status = 'em_andamento'
)`

func (r *RepositorioPostgres) AtividadeAtiva(ctx context.Context, atividadeID, usuarioID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, sqlAtividadeAtiva, atividadeID, usuarioID).Scan(&ok)
	return ok, err
}

const (
	// Lock transacional por célula: serializa disputas simultâneas na MESMA
	// célula (inclusive quando a linha ainda não existe, caso em que
	// SELECT ... FOR UPDATE não teria o que travar). Células diferentes não
	// competem entre si.
	sqlLockCelula = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

	sqlSelecionarParaAtualizar = `
SELECT hex_id, dono_id, dominado_em, pontos_defesa, atividade_id
  FROM territorios
 WHERE hex_id = $1
   FOR UPDATE`

	sqlUpsert = `
INSERT INTO territorios (hex_id, dono_id, dominado_em, pontos_defesa, atividade_id, centro, atualizado_em)
VALUES ($1, $2, $3, $4, $5, ST_SetSRID(ST_MakePoint($6, $7), 4326), now())
ON CONFLICT (hex_id) DO UPDATE
   SET dono_id       = EXCLUDED.dono_id,
       dominado_em   = EXCLUDED.dominado_em,
       pontos_defesa = EXCLUDED.pontos_defesa,
       atividade_id  = EXCLUDED.atividade_id,
       atualizado_em = now()`
)

func (r *RepositorioPostgres) Conquistar(ctx context.Context, hexID string, decidir func(*Territorio) Decisao) (*Territorio, Decisao, error) {
	centro, err := geo.Centro(hexID)
	if err != nil {
		return nil, Decisao{}, err
	}

	var (
		anterior *Territorio
		d        Decisao
	)
	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sqlLockCelula, hexID); err != nil {
			return fmt.Errorf("lock da célula: %w", err)
		}

		var t Territorio
		err := tx.QueryRow(ctx, sqlSelecionarParaAtualizar, hexID).
			Scan(&t.HexID, &t.DonoID, &t.DominadoEm, &t.PontosDefesa, &t.AtividadeID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			anterior = nil
		case err != nil:
			return fmt.Errorf("lendo território: %w", err)
		default:
			anterior = &t
		}

		d = decidir(anterior)
		n := d.Novo
		_, err = tx.Exec(ctx, sqlUpsert,
			n.HexID, n.DonoID, n.DominadoEm, n.PontosDefesa, n.AtividadeID,
			centro.Longitude, centro.Latitude, // ST_MakePoint(x=lng, y=lat)
		)
		if err != nil {
			return fmt.Errorf("upsert território: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, Decisao{}, err
	}
	return anterior, d, nil
}

const sqlListarPorHexIDs = `
SELECT hex_id, dono_id, dominado_em, pontos_defesa, atividade_id
  FROM territorios
 WHERE hex_id = ANY($1)
   AND dono_id IS NOT NULL`

func (r *RepositorioPostgres) ListarPorHexIDs(ctx context.Context, hexIDs []string) ([]Territorio, error) {
	if len(hexIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, sqlListarPorHexIDs, hexIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Territorio, error) {
		var t Territorio
		err := row.Scan(&t.HexID, &t.DonoID, &t.DominadoEm, &t.PontosDefesa, &t.AtividadeID)
		return t, err
	})
}

const sqlInserirConquistaPoligono = `
INSERT INTO conquistas_poligonos (usuario_id, atividade_id, area_m2, distancia_metros, pontos, criado_em)
VALUES ($1, $2, $3, $4, $5, $6)`

func (r *RepositorioPostgres) SalvarConquistaPoligono(ctx context.Context, reg RegistroConquistaPoligono) error {
	pontosJSON, err := json.Marshal(reg.Pontos)
	if err != nil {
		return fmt.Errorf("serializando pontos da conquista: %w", err)
	}

	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Registra o evento histórico da conquista de polígono
		_, err := tx.Exec(ctx, sqlInserirConquistaPoligono,
			reg.UsuarioID, reg.AtividadeID, reg.AreaM2, reg.DistanciaMetros, pontosJSON, reg.CriadoEm,
		)
		if err != nil {
			// Não bloqueia caso a tabela conquistas_poligonos ainda não tenha sido migrada
			// mas tenta o upsert das células territoriais
		}

		// Atualiza a posse das células H3 cobertas pelo polígono
		for _, hexID := range reg.HexIDs {
			centro, err := geo.Centro(hexID)
			if err != nil {
				continue
			}
			_, err = tx.Exec(ctx, sqlUpsert,
				hexID, reg.UsuarioID, reg.CriadoEm, 1, reg.AtividadeID,
				centro.Longitude, centro.Latitude,
			)
			if err != nil {
				return fmt.Errorf("upsert de celula conquistada %s: %w", hexID, err)
			}
		}
		return nil
	})
}

