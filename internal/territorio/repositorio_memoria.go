package territorio

import (
	"context"
	"sync"
)

// RepositorioMemoria é uma implementação thread-safe em memória, usada em
// testes e para rodar a API localmente sem banco (ZONEX_DB=memoria).
type RepositorioMemoria struct {
	mu                  sync.Mutex
	territorios         map[string]Territorio
	// atividadesAtivas: atividadeID -> usuarioID dono da atividade em andamento.
	atividadesAtivas    map[int64]int64
	conquistasPoligonos []RegistroConquistaPoligono
}

func NovoRepositorioMemoria() *RepositorioMemoria {
	return &RepositorioMemoria{
		territorios:      make(map[string]Territorio),
		atividadesAtivas: make(map[int64]int64),
	}
}

// IniciarAtividade marca uma atividade como em andamento para o usuário.
func (m *RepositorioMemoria) IniciarAtividade(atividadeID, usuarioID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.atividadesAtivas[atividadeID] = usuarioID
}

// Semear grava um território diretamente (útil para montar cenários de teste).
func (m *RepositorioMemoria) Semear(t Territorio) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.territorios[t.HexID] = t
}

func (m *RepositorioMemoria) Obter(hexID string) (Territorio, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.territorios[hexID]
	return t, ok
}

func (m *RepositorioMemoria) AtividadeAtiva(_ context.Context, atividadeID, usuarioID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dono, ok := m.atividadesAtivas[atividadeID]
	return ok && dono == usuarioID, nil
}

func (m *RepositorioMemoria) Conquistar(_ context.Context, hexID string, decidir func(*Territorio) Decisao) (*Territorio, Decisao, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var anterior *Territorio
	if t, ok := m.territorios[hexID]; ok {
		c := t
		anterior = &c
	}
	d := decidir(anterior)
	m.territorios[hexID] = d.Novo
	return anterior, d, nil
}

func (m *RepositorioMemoria) ListarPorHexIDs(_ context.Context, hexIDs []string) ([]Territorio, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Territorio
	for _, id := range hexIDs {
		if t, ok := m.territorios[id]; ok && t.DonoID != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *RepositorioMemoria) SalvarConquistaPoligono(_ context.Context, reg RegistroConquistaPoligono) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conquistasPoligonos = append(m.conquistasPoligonos, reg)
	for _, hexID := range reg.HexIDs {
		dono := reg.UsuarioID
		t := Territorio{
			HexID:        hexID,
			DonoID:       &dono,
			DominadoEm:   reg.CriadoEm,
			PontosDefesa: 1,
			AtividadeID:  reg.AtividadeID,
		}
		m.territorios[hexID] = t
	}
	return nil
}

func (m *RepositorioMemoria) ListarConquistasPoligono() []RegistroConquistaPoligono {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RegistroConquistaPoligono, len(m.conquistasPoligonos))
	copy(out, m.conquistasPoligonos)
	return out
}

