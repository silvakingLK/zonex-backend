package territorio

import (
	"testing"
	"time"
)

func ptr(v int64) *int64 { return &v }

func TestResolverDisputa(t *testing.T) {
	agora := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	antes := agora.Add(-48 * time.Hour)
	const hex, eu, rival, ativ, max = "89a8100c2d3ffff", int64(1), int64(2), int64(77), 5

	casos := []struct {
		nome       string
		atual      *Territorio
		status     StatusConquista
		dono       int64
		defesa     int
		dominadoEm time.Time
	}{
		{"célula nova", nil, StatusConquistado, eu, 1, agora},
		{"célula sem dono", &Territorio{HexID: hex, DonoID: nil, PontosDefesa: 3, DominadoEm: antes}, StatusConquistado, eu, 1, agora},
		{"reforço próprio", &Territorio{HexID: hex, DonoID: ptr(eu), PontosDefesa: 2, DominadoEm: antes}, StatusReforcado, eu, 3, antes},
		{"reforço no teto", &Territorio{HexID: hex, DonoID: ptr(eu), PontosDefesa: max, DominadoEm: antes}, StatusReforcado, eu, max, antes},
		{"ataque com defesa alta", &Territorio{HexID: hex, DonoID: ptr(rival), PontosDefesa: 3, DominadoEm: antes}, StatusAtacado, rival, 2, antes},
		{"tomada com defesa 1", &Territorio{HexID: hex, DonoID: ptr(rival), PontosDefesa: 1, DominadoEm: antes}, StatusTomado, eu, 1, agora},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var copiaAntes Territorio
			if c.atual != nil {
				copiaAntes = *c.atual
			}
			d := ResolverDisputa(c.atual, hex, eu, ativ, agora, max)

			if d.Status != c.status {
				t.Errorf("status = %s, esperado %s", d.Status, c.status)
			}
			if d.Novo.DonoID == nil || *d.Novo.DonoID != c.dono {
				t.Errorf("dono = %v, esperado %d", d.Novo.DonoID, c.dono)
			}
			if d.Novo.PontosDefesa != c.defesa {
				t.Errorf("defesa = %d, esperado %d", d.Novo.PontosDefesa, c.defesa)
			}
			if !d.Novo.DominadoEm.Equal(c.dominadoEm) {
				t.Errorf("dominado_em = %v, esperado %v", d.Novo.DominadoEm, c.dominadoEm)
			}
			if d.Novo.HexID != hex {
				t.Errorf("hex = %s", d.Novo.HexID)
			}
			if c.atual != nil && *c.atual != copiaAntes {
				t.Errorf("ResolverDisputa não deve mutar o estado de entrada")
			}
		})
	}
}
