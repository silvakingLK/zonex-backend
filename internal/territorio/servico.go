package territorio

import (
	"context"
	"fmt"
	"time"

	"github.com/zonex/backend/internal/geo"
)

// Config parametriza o motor de conquista.
type Config struct {
	Resolucao  int // resolução H3 (padrão 9)
	MaxDefesa  int // teto de pontos_defesa (padrão 5)
	MaxCelulas int // máximo de células por consulta de área (padrão 2500)
}

func (c Config) comPadroes() Config {
	if c.Resolucao == 0 {
		c.Resolucao = geo.ResolucaoPadrao
	}
	if c.MaxDefesa <= 0 {
		c.MaxDefesa = 5
	}
	if c.MaxCelulas <= 0 {
		c.MaxCelulas = 2500
	}
	return c
}

// Servico orquestra validações, H3 e persistência.
type Servico struct {
	repo  Repositorio
	cfg   Config
	agora func() time.Time
}

func NovoServico(repo Repositorio, cfg Config) *Servico {
	return &Servico{repo: repo, cfg: cfg.comPadroes(), agora: func() time.Time { return time.Now().UTC() }}
}

// ---------- Conquista ----------

type ConquistaEntrada struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	AtividadeID int64   `json:"atividade_id"`
}

type ResultadoConquista struct {
	HexID          string           `json:"hex_id"`
	Status         StatusConquista  `json:"status"`
	DonoID         *int64           `json:"dono_id"`
	DonoAnteriorID *int64           `json:"dono_anterior_id"`
	PontosDefesa   int              `json:"pontos_defesa"`
	DominadoEm     time.Time        `json:"dominado_em"`
	Poligono       []geo.Coordenada `json:"poligono"`
}

func (s *Servico) Conquistar(ctx context.Context, usuarioID int64, in ConquistaEntrada) (ResultadoConquista, error) {
	if usuarioID <= 0 {
		return ResultadoConquista{}, ErrNaoAutenticado
	}
	if in.AtividadeID <= 0 {
		return ResultadoConquista{}, fmt.Errorf("%w: atividade_id obrigatório", ErrRequisicao)
	}
	hexID, err := geo.ParaHex(in.Latitude, in.Longitude, s.cfg.Resolucao)
	if err != nil {
		return ResultadoConquista{}, fmt.Errorf("%w: %v", ErrRequisicao, err)
	}

	ativa, err := s.repo.AtividadeAtiva(ctx, in.AtividadeID, usuarioID)
	if err != nil {
		return ResultadoConquista{}, fmt.Errorf("verificando atividade: %w", err)
	}
	if !ativa {
		return ResultadoConquista{}, ErrAtividadeInativa
	}

	agora := s.agora()
	anterior, d, err := s.repo.Conquistar(ctx, hexID, func(atual *Territorio) Decisao {
		return ResolverDisputa(atual, hexID, usuarioID, in.AtividadeID, agora, s.cfg.MaxDefesa)
	})
	if err != nil {
		return ResultadoConquista{}, fmt.Errorf("conquistando %s: %w", hexID, err)
	}

	poligono, _ := geo.Poligono(hexID) // hexID gerado acima é sempre válido
	res := ResultadoConquista{
		HexID:        hexID,
		Status:       d.Status,
		DonoID:       d.Novo.DonoID,
		PontosDefesa: d.Novo.PontosDefesa,
		DominadoEm:   d.Novo.DominadoEm,
		Poligono:     poligono,
	}
	if anterior != nil {
		res.DonoAnteriorID = anterior.DonoID
	}
	return res, nil
}

// ---------- Visíveis ----------

// AreaConsulta aceita OU bounds (min/max) OU centro + raio.
type AreaConsulta struct {
	MinLat, MinLng, MaxLat, MaxLng float64
	Lat, Lng, RaioM                float64
	PorRaio                        bool
}

const (
	StatusNeutro   = "neutro"
	StatusDominado = "dominado"
)

type HexVisivel struct {
	HexID        string           `json:"hex_id"`
	Status       string           `json:"status"`
	DonoID       *int64           `json:"dono_id"`
	PontosDefesa int              `json:"pontos_defesa"`
	DominadoEm   *time.Time       `json:"dominado_em"`
	Poligono     []geo.Coordenada `json:"poligono"`
}

type RespostaVisiveis struct {
	Resolucao int          `json:"resolucao"`
	Total     int          `json:"total"`
	Dominados int          `json:"dominados"`
	Hexagonos []HexVisivel `json:"hexagonos"`
}

func (s *Servico) Visiveis(ctx context.Context, a AreaConsulta) (RespostaVisiveis, error) {
	var (
		ids []string
		err error
	)
	if a.PorRaio {
		ids, err = geo.CelulasNoRaio(a.Lat, a.Lng, a.RaioM, s.cfg.Resolucao, s.cfg.MaxCelulas)
	} else {
		ids, err = geo.CelulasNoRetangulo(a.MinLat, a.MinLng, a.MaxLat, a.MaxLng, s.cfg.Resolucao, s.cfg.MaxCelulas)
	}
	if err != nil {
		return RespostaVisiveis{}, fmt.Errorf("%w: %v", ErrRequisicao, err)
	}

	dominados, err := s.repo.ListarPorHexIDs(ctx, ids)
	if err != nil {
		return RespostaVisiveis{}, fmt.Errorf("listando territórios: %w", err)
	}
	porHex := make(map[string]Territorio, len(dominados))
	for _, t := range dominados {
		porHex[t.HexID] = t
	}

	resp := RespostaVisiveis{Resolucao: s.cfg.Resolucao, Total: len(ids), Hexagonos: make([]HexVisivel, 0, len(ids))}
	for _, id := range ids {
		poligono, _ := geo.Poligono(id)
		h := HexVisivel{HexID: id, Status: StatusNeutro, Poligono: poligono}
		if t, ok := porHex[id]; ok && t.DonoID != nil {
			dom := t.DominadoEm
			h.Status, h.DonoID, h.PontosDefesa, h.DominadoEm = StatusDominado, t.DonoID, t.PontosDefesa, &dom
			resp.Dominados++
		}
		resp.Hexagonos = append(resp.Hexagonos, h)
	}
	return resp, nil
}
