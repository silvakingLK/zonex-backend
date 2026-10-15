package territorio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zonex/backend/internal/geo"
)

const (
	tLat = -23.561414 // Av. Paulista
	tLng = -46.655881
)

type ambiente struct {
	repo   *RepositorioMemoria
	router http.Handler
	agora  time.Time
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	repo := NovoRepositorioMemoria()
	svc := NovoServico(repo, Config{MaxDefesa: 3, MaxCelulas: 1000})
	agora := time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)
	svc.agora = func() time.Time { return agora }

	r := chi.NewRouter()
	r.Use(MiddlewareUsuarioHeaderDev)
	r.Route("/api/territorios", NovoHandler(svc, nil).Rotas)
	return &ambiente{repo: repo, router: r, agora: agora}
}

func (a *ambiente) conquistar(t *testing.T, usuario int64, corpo any) (*httptest.ResponseRecorder, ResultadoConquista) {
	t.Helper()
	var buf bytes.Buffer
	switch v := corpo.(type) {
	case string:
		buf.WriteString(v)
	default:
		_ = json.NewEncoder(&buf).Encode(v)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/territorios/conquistar", &buf)
	req.Header.Set("Content-Type", "application/json")
	if usuario > 0 {
		req.Header.Set("X-Usuario-ID", strconv.FormatInt(usuario, 10))
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	var res ResultadoConquista
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("resposta não é JSON válido: %v — %s", err, rec.Body.String())
		}
	}
	return rec, res
}

func TestConquistar_FluxoCompletoDeDisputa(t *testing.T) {
	a := novoAmbiente(t)
	const alice, bob = int64(1), int64(2)
	a.repo.IniciarAtividade(10, alice)
	a.repo.IniciarAtividade(20, bob)
	esperadoHex, _ := geo.ParaHex(tLat, tLng, geo.ResolucaoPadrao)

	passo := func(usuario, ativ int64, status StatusConquista, dono int64, defesa int) ResultadoConquista {
		t.Helper()
		rec, res := a.conquistar(t, usuario, ConquistaEntrada{Latitude: tLat, Longitude: tLng, AtividadeID: ativ})
		if rec.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
		}
		if res.HexID != esperadoHex || res.Status != status || res.DonoID == nil || *res.DonoID != dono || res.PontosDefesa != defesa {
			t.Fatalf("resultado inesperado: hex=%s status=%s dono=%v defesa=%d (esperado %s/%s/%d/%d)",
				res.HexID, res.Status, deref(res.DonoID), res.PontosDefesa, esperadoHex, status, dono, defesa)
		}
		if len(res.Poligono) != 6 {
			t.Fatalf("polígono deveria ter 6 vértices, tem %d", len(res.Poligono))
		}
		return res
	}

	r := passo(alice, 10, StatusConquistado, alice, 1)
	if r.DonoAnteriorID != nil {
		t.Fatalf("célula nova não deveria ter dono anterior")
	}
	passo(alice, 10, StatusReforcado, alice, 2)
	passo(alice, 10, StatusReforcado, alice, 3)
	passo(alice, 10, StatusReforcado, alice, 3) // teto MaxDefesa=3
	passo(bob, 20, StatusAtacado, alice, 2)
	passo(bob, 20, StatusAtacado, alice, 1)
	r = passo(bob, 20, StatusTomado, bob, 1)
	if r.DonoAnteriorID == nil || *r.DonoAnteriorID != alice {
		t.Fatalf("dono anterior = %v, esperado %d", deref(r.DonoAnteriorID), alice)
	}
	if !r.DominadoEm.Equal(a.agora) {
		t.Fatalf("dominado_em = %v, esperado %v", r.DominadoEm, a.agora)
	}

	// Estado persistido confere com a resposta.
	salvo, ok := a.repo.Obter(esperadoHex)
	if !ok || *salvo.DonoID != bob || salvo.PontosDefesa != 1 || *salvo.AtividadeID != 20 {
		t.Fatalf("estado persistido incorreto: %+v", salvo)
	}
}

func TestConquistar_ErrosDeValidacao(t *testing.T) {
	a := novoAmbiente(t)
	a.repo.IniciarAtividade(10, 1)

	casos := []struct {
		nome    string
		usuario int64
		corpo   any
		codigo  int
	}{
		{"sem autenticação", 0, ConquistaEntrada{tLat, tLng, 10}, http.StatusUnauthorized},
		{"JSON malformado", 1, `{"latitude": `, http.StatusBadRequest},
		{"campo desconhecido", 1, `{"latitude":1,"longitude":1,"atividade_id":10,"hack":true}`, http.StatusBadRequest},
		{"latitude inválida", 1, ConquistaEntrada{123, tLng, 10}, http.StatusBadRequest},
		{"longitude inválida", 1, ConquistaEntrada{tLat, -200, 10}, http.StatusBadRequest},
		{"sem atividade_id", 1, ConquistaEntrada{tLat, tLng, 0}, http.StatusBadRequest},
		{"atividade inexistente", 1, ConquistaEntrada{tLat, tLng, 999}, http.StatusForbidden},
		{"atividade de outro usuário", 2, ConquistaEntrada{tLat, tLng, 10}, http.StatusForbidden},
		{"corpo gigante", 1, `{"latitude":1,"longitude":1,"atividade_id":10,"x":"` + string(bytes.Repeat([]byte("a"), 10_000)) + `"}`, http.StatusBadRequest},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec, _ := a.conquistar(t, c.usuario, c.corpo)
			if rec.Code != c.codigo {
				t.Fatalf("HTTP %d, esperado %d — %s", rec.Code, c.codigo, rec.Body.String())
			}
			var corpoErro map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &corpoErro); err != nil || corpoErro["erro"] == "" {
				t.Fatalf("resposta de erro deveria ter campo 'erro': %s", rec.Body.String())
			}
		})
	}
	hex, _ := geo.ParaHex(tLat, tLng, geo.ResolucaoPadrao)
	if _, ok := a.repo.Obter(hex); ok {
		t.Fatal("nenhuma requisição inválida deveria ter persistido território")
	}
}

func TestConquistar_Concorrente(t *testing.T) {
	a := novoAmbiente(t)
	const n = 50
	for i := int64(1); i <= n; i++ {
		a.repo.IniciarAtividade(100+i, i)
	}
	var wg sync.WaitGroup
	for i := int64(1); i <= n; i++ {
		wg.Add(1)
		go func(u int64) {
			defer wg.Done()
			rec, _ := a.conquistar(t, u, ConquistaEntrada{tLat, tLng, 100 + u})
			if rec.Code != http.StatusOK {
				t.Errorf("HTTP %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()
	hex, _ := geo.ParaHex(tLat, tLng, geo.ResolucaoPadrao)
	salvo, ok := a.repo.Obter(hex)
	if !ok || salvo.DonoID == nil || salvo.PontosDefesa < 1 {
		t.Fatalf("estado final inválido após disputa concorrente: %+v", salvo)
	}
}

func TestVisiveis(t *testing.T) {
	a := novoAmbiente(t)
	hex, _ := geo.ParaHex(tLat, tLng, geo.ResolucaoPadrao)
	a.repo.Semear(Territorio{HexID: hex, DonoID: ptr(42), PontosDefesa: 2, DominadoEm: a.agora})

	urls := map[string]string{
		"bounds": fmt.Sprintf("/api/territorios/visiveis?min_lat=%f&min_lng=%f&max_lat=%f&max_lng=%f", tLat-0.004, tLng-0.004, tLat+0.004, tLng+0.004),
		"raio":   fmt.Sprintf("/api/territorios/visiveis?lat=%f&lng=%f&raio=400", tLat, tLng),
	}
	for nome, url := range urls {
		t.Run(nome, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}
			var resp RespostaVisiveis
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Total < 5 || resp.Total != len(resp.Hexagonos) || resp.Dominados != 1 {
				t.Fatalf("total=%d len=%d dominados=%d", resp.Total, len(resp.Hexagonos), resp.Dominados)
			}
			achou := false
			for _, h := range resp.Hexagonos {
				if len(h.Poligono) < 5 {
					t.Fatalf("hex %s sem polígono", h.HexID)
				}
				if h.HexID == hex {
					achou = true
					if h.Status != StatusDominado || deref(h.DonoID) != 42 || h.PontosDefesa != 2 || h.DominadoEm == nil {
						t.Fatalf("hex dominado com dados errados: %+v", h)
					}
				} else if h.Status != StatusNeutro || h.DonoID != nil {
					t.Fatalf("hex %s deveria ser neutro: %+v", h.HexID, h)
				}
			}
			if !achou {
				t.Fatal("hex dominado não retornado")
			}
		})
	}
}

func TestVisiveis_ParametrosInvalidos(t *testing.T) {
	a := novoAmbiente(t)
	for _, q := range []string{
		"",                               // nada informado
		"?min_lat=1&min_lng=1&max_lat=2", // faltando max_lng
		"?min_lat=x&min_lng=1&max_lat=2&max_lng=2",         // não numérico
		"?min_lat=2&min_lng=1&max_lat=1&max_lng=2",         // min > max
		"?min_lat=-24&min_lng=-47&max_lat=-23&max_lng=-46", // área gigante
		"?lat=-23.5&lng=-46.6",                             // sem raio
		"?lat=-23.5&lng=-46.6&raio=-5",                     // raio negativo
	} {
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/territorios/visiveis"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: HTTP %d, esperado 400 — %s", q, rec.Code, rec.Body.String())
		}
	}
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (a *ambiente) conquistarPoligono(t *testing.T, usuarioHeader string, corpo any) (*httptest.ResponseRecorder, ConquerPolygonResponse) {
	t.Helper()
	var buf bytes.Buffer
	switch v := corpo.(type) {
	case string:
		buf.WriteString(v)
	default:
		_ = json.NewEncoder(&buf).Encode(v)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/territorios/conquistar-poligono", &buf)
	req.Header.Set("Content-Type", "application/json")
	if usuarioHeader != "" {
		req.Header.Set("X-Usuario-ID", usuarioHeader)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	var res ConquerPolygonResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec, res
}

func TestConquistarPoligono_Sucesso(t *testing.T) {
	a := novoAmbiente(t)

	// Polígono fechado em torno da Av. Paulista
	pontos := []PointDto{
		{Latitude: tLat, Longitude: tLng},
		{Latitude: tLat, Longitude: tLng + 0.002},
		{Latitude: tLat + 0.002, Longitude: tLng + 0.002},
		{Latitude: tLat + 0.002, Longitude: tLng},
	}

	payload := ConquerPolygonRequest{
		AtividadeID:     10,
		Pontos:          pontos,
		DistanciaMetros: 500.5,
	}

	// 1. Testa com X-Usuario-ID informado
	rec, res := a.conquistarPoligono(t, "42", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if !res.Sucesso {
		t.Fatalf("esperava sucesso=true, obteve false. Mensagem: %s", res.Mensagem)
	}
	if res.AreaM2 <= 0 {
		t.Fatalf("esperava areaM2 > 0, obteve %f", res.AreaM2)
	}

	conquistas := a.repo.ListarConquistasPoligono()
	if len(conquistas) != 1 {
		t.Fatalf("esperava 1 conquista salva no repositório, obteve %d", len(conquistas))
	}
	if conquistas[0].UsuarioID != 42 {
		t.Errorf("usuario_id = %d, esperado 42", conquistas[0].UsuarioID)
	}
	if conquistas[0].AreaM2 != res.AreaM2 {
		t.Errorf("area_m2 salva = %f, resposta = %f", conquistas[0].AreaM2, res.AreaM2)
	}

	// 2. Testa sem X-Usuario-ID (deve assumir ID padrão 1)
	rec2, res2 := a.conquistarPoligono(t, "", payload)
	if rec2.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d", rec2.Code)
	}
	if !res2.Sucesso {
		t.Fatalf("esperava sucesso=true, obteve false")
	}
	conquistas2 := a.repo.ListarConquistasPoligono()
	if len(conquistas2) != 2 || conquistas2[1].UsuarioID != 1 {
		t.Fatalf("esperava conquista com usuarioID padrão 1, obteve %+v", conquistas2[1])
	}

	// 3. Testa com X-Usuario-ID inválido (erro de parse -> assume padrão 1)
	rec3, res3 := a.conquistarPoligono(t, "nao-eh-numero", payload)
	if rec3.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d", rec3.Code)
	}
	if !res3.Sucesso {
		t.Fatalf("esperava sucesso=true, obteve false")
	}
	conquistas3 := a.repo.ListarConquistasPoligono()
	if len(conquistas3) != 3 || conquistas3[2].UsuarioID != 1 {
		t.Fatalf("esperava usuarioID padrão 1 após erro de parse, obteve %+v", conquistas3[2])
	}
}

func TestConquistarPoligono_Validacoes(t *testing.T) {
	a := novoAmbiente(t)

	casos := []struct {
		nome        string
		corpo       any
		esperaErro  string
		esperaCode  int
	}{
		{
			nome: "menos de 3 coordenadas (apenas 2 pontos)",
			corpo: ConquerPolygonRequest{
				AtividadeID: 1,
				Pontos: []PointDto{
					{Latitude: tLat, Longitude: tLng},
					{Latitude: tLat + 0.001, Longitude: tLng + 0.001},
				},
			},
			esperaCode: http.StatusBadRequest,
		},
		{
			nome: "lista de pontos vazia",
			corpo: ConquerPolygonRequest{
				AtividadeID: 1,
				Pontos:      []PointDto{},
			},
			esperaCode: http.StatusBadRequest,
		},
		{
			nome:       "JSON malformatado",
			corpo:      `{"pontos": [{"latitude": }`,
			esperaCode: http.StatusBadRequest,
		},
		{
			nome: "coordenada geográfica inválida (lat > 90)",
			corpo: ConquerPolygonRequest{
				AtividadeID: 1,
				Pontos: []PointDto{
					{Latitude: 95.0, Longitude: tLng},
					{Latitude: tLat, Longitude: tLng + 0.001},
					{Latitude: tLat + 0.001, Longitude: tLng + 0.001},
				},
			},
			esperaCode: http.StatusBadRequest,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec, res := a.conquistarPoligono(t, "1", c.corpo)
			if rec.Code != c.esperaCode {
				t.Fatalf("HTTP %d, esperado %d — corpo: %s", rec.Code, c.esperaCode, rec.Body.String())
			}
			if res.Sucesso {
				t.Fatalf("esperava sucesso=false para caso de erro")
			}
			if res.Mensagem == "" {
				t.Fatalf("esperava mensagem explicativa de erro no JSON")
			}
		})
	}
}

func TestConquistarPoligono_MetodoNaoPermitido(t *testing.T) {
	a := novoAmbiente(t)
	req := httptest.NewRequest(http.MethodGet, "/api/territorios/conquistar-poligono", nil)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperava HTTP 405 Method Not Allowed, obteve %d", rec.Code)
	}
}

