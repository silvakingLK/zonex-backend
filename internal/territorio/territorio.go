// Package territorio implementa o motor de disputa/conquista de células
// hexagonais do ZoneX.
package territorio

import (
	"context"
	"errors"
	"time"
)

// Territorio representa uma célula H3 dominada.
// DonoID nulo significa célula neutra (ex: dono excluído).
type Territorio struct {
	HexID        string    `json:"hex_id"`
	DonoID       *int64    `json:"dono_id"`
	DominadoEm   time.Time `json:"dominado_em"`
	PontosDefesa int       `json:"pontos_defesa"`
	AtividadeID  *int64    `json:"atividade_id,omitempty"`
}

// StatusConquista descreve o efeito de uma tentativa de conquista.
type StatusConquista string

const (
	// StatusConquistado: célula neutra passou a ser do usuário.
	StatusConquistado StatusConquista = "conquistado"
	// StatusTomado: célula de outro jogador (com 1 ponto de defesa) foi tomada.
	StatusTomado StatusConquista = "tomado"
	// StatusAtacado: célula inimiga perdeu 1 ponto de defesa, mas continua com o dono.
	StatusAtacado StatusConquista = "atacado"
	// StatusReforcado: célula já era do usuário e ganhou +1 de defesa (até o máximo).
	StatusReforcado StatusConquista = "reforcado"
)

var (
	ErrAtividadeInativa = errors.New("atividade não encontrada, não pertence ao usuário ou não está em andamento")
	ErrNaoAutenticado   = errors.New("usuário não autenticado")
	ErrRequisicao       = errors.New("requisição inválida")
)

// Decisao é o resultado puro (sem I/O) da resolução de uma disputa.
type Decisao struct {
	Novo   Territorio
	Status StatusConquista
}

// ResolverDisputa aplica as regras do jogo. É uma função pura para ser
// facilmente testável e executada dentro da transação do repositório.
//
// Regras:
//   - célula inexistente ou sem dono  -> usuário conquista com 1 de defesa;
//   - célula do próprio usuário       -> +1 de defesa (limitado a maxDefesa);
//   - célula inimiga com defesa > 1   -> defesa -1, dono mantido;
//   - célula inimiga com defesa <= 1  -> usuário toma a célula com 1 de defesa.
//
// dominado_em só muda quando a célula troca de dono.
func ResolverDisputa(atual *Territorio, hexID string, usuarioID, atividadeID int64, agora time.Time, maxDefesa int) Decisao {
	if maxDefesa < 1 {
		maxDefesa = 1
	}
	ativ := atividadeID
	usuario := usuarioID
	novoDono := Territorio{
		HexID:        hexID,
		DonoID:       &usuario,
		DominadoEm:   agora,
		PontosDefesa: 1,
		AtividadeID:  &ativ,
	}

	if atual == nil || atual.DonoID == nil {
		return Decisao{Novo: novoDono, Status: StatusConquistado}
	}

	t := *atual // cópia: nunca mutar o estado lido
	if *t.DonoID == usuarioID {
		if t.PontosDefesa < maxDefesa {
			t.PontosDefesa++
		}
		t.AtividadeID = &ativ
		return Decisao{Novo: t, Status: StatusReforcado}
	}

	if t.PontosDefesa > 1 {
		t.PontosDefesa--
		return Decisao{Novo: t, Status: StatusAtacado}
	}
	return Decisao{Novo: novoDono, Status: StatusTomado}
}

// Repositorio abstrai a persistência (PostgreSQL em produção, memória nos testes).
type Repositorio interface {
	// AtividadeAtiva verifica se a atividade existe, pertence ao usuário e está em andamento.
	AtividadeAtiva(ctx context.Context, atividadeID, usuarioID int64) (bool, error)

	// Conquistar executa, de forma atômica e serializada por célula, a leitura
	// do estado atual, a decisão (callback puro) e o UPSERT do novo estado.
	// Retorna o estado anterior (nil se a célula não existia) e a decisão aplicada.
	Conquistar(ctx context.Context, hexID string, decidir func(atual *Territorio) Decisao) (anterior *Territorio, d Decisao, err error)

	// ListarPorHexIDs retorna os territórios dominados dentre os IDs informados.
	ListarPorHexIDs(ctx context.Context, hexIDs []string) ([]Territorio, error)

	// SalvarConquistaPoligono registra a conquista de polígono e atualiza a posse das células cobertas.
	SalvarConquistaPoligono(ctx context.Context, reg RegistroConquistaPoligono) error
}

// ---------- DTOs para Conquista de Territórios Livres por Polígono (Paper.io Mode) ----------

// PointDto representa uma coordenada geográfica enviada pelo app.
type PointDto struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ConquerPolygonRequest recebe o payload de conquista por polígono.
type ConquerPolygonRequest struct {
	AtividadeID     int64      `json:"atividadeId"`
	Pontos          []PointDto `json:"pontos"`
	DistanciaMetros float32    `json:"distanciaMetros"`
}

// ConquerPolygonResponse retorna o resultado da operação de conquista ao cliente.
type ConquerPolygonResponse struct {
	Sucesso  bool    `json:"sucesso"`
	Mensagem string  `json:"mensagem"`
	AreaM2   float64 `json:"areaM2,omitempty"`
}

// RegistroConquistaPoligono representa a persistência do polígono conquistado.
type RegistroConquistaPoligono struct {
	ID              int64      `json:"id,omitempty"`
	UsuarioID       int64      `json:"usuario_id"`
	AtividadeID     *int64     `json:"atividade_id,omitempty"`
	AreaM2          float64    `json:"area_m2"`
	DistanciaMetros float32    `json:"distancia_metros"`
	Pontos          []PointDto `json:"pontos"`
	HexIDs          []string   `json:"hex_ids,omitempty"`
	CriadoEm        time.Time  `json:"criado_em"`
}

