package territorio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/zonex/backend/internal/geo"
)

// ---------- Contexto de autenticação ----------

type ctxChave struct{}

// ComUsuario injeta o ID do usuário autenticado no contexto. Deve ser chamado
// pelo middleware de autenticação real do projeto (JWT/sessão).
func ComUsuario(ctx context.Context, usuarioID int64) context.Context {
	return context.WithValue(ctx, ctxChave{}, usuarioID)
}

// UsuarioDoContexto devolve o usuário autenticado (ok=false se ausente).
func UsuarioDoContexto(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxChave{}).(int64)
	return id, ok && id > 0
}

// MiddlewareUsuarioHeaderDev lê o usuário do header X-Usuario-ID.
// ⚠️ APENAS PARA DESENVOLVIMENTO: substitua pelo middleware JWT do ZoneX,
// que deve chamar ComUsuario(ctx, claims.UserID).
func MiddlewareUsuarioHeaderDev(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Usuario-ID"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
				r = r.WithContext(ComUsuario(r.Context(), id))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- Handler ----------

const tamanhoMaxCorpo = 4 << 10 // 4 KiB é mais que suficiente para o payload de conquista

type Handler struct {
	svc *Servico
	log *slog.Logger
}

func NovoHandler(svc *Servico, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// Rotas registra os endpoints sob o router informado (ex: montado em /api/territorios).
func (h *Handler) Rotas(r chi.Router) {
	r.Get("/visiveis", h.Visiveis)
	r.Post("/conquistar", h.Conquistar)
	r.Post("/conquistar-poligono", h.ConquistarPoligonoHandler)
}

// Conquistar trata POST /api/territorios/conquistar.
func (h *Handler) Conquistar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := UsuarioDoContexto(r.Context())
	if !ok {
		escreverErro(w, http.StatusUnauthorized, ErrNaoAutenticado.Error())
		return
	}

	var in ConquistaEntrada
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, tamanhoMaxCorpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		escreverErro(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}

	res, err := h.svc.Conquistar(r.Context(), usuarioID, in)
	if err != nil {
		h.tratarErro(w, r, err)
		return
	}
	escreverJSON(w, http.StatusOK, res)
}

// ConquistarPoligonoHandler trata POST /api/territorios/conquistar-poligono (Paper.io Mode).
func (h *Handler) ConquistarPoligonoHandler(w http.ResponseWriter, r *http.Request) {
	// Validação explícita do método HTTP
	if r.Method != http.MethodPost {
		escreverJSON(w, http.StatusMethodNotAllowed, ConquerPolygonResponse{
			Sucesso:  false,
			Mensagem: "método não permitido, esperado POST",
		})
		return
	}

	// Obtenha o cabeçalho X-Usuario-ID (utilize ID padrão 1 caso não seja informado ou ocorra erro de parse)
	usuarioID := int64(1)
	if v := r.Header.Get("X-Usuario-ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			usuarioID = id
		}
	} else if id, ok := UsuarioDoContexto(r.Context()); ok && id > 0 {
		usuarioID = id
	}

	var req ConquerPolygonRequest
	// Permite até 1 MB para polígonos com muitos pontos GPS
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		escreverJSON(w, http.StatusBadRequest, ConquerPolygonResponse{
			Sucesso:  false,
			Mensagem: "corpo da requisição JSON inválido: " + err.Error(),
		})
		return
	}

	// Valida se a lista de pontos contém ao menos 3 coordenadas para formar uma área fechada válida
	if len(req.Pontos) < 3 {
		escreverJSON(w, http.StatusBadRequest, ConquerPolygonResponse{
			Sucesso:  false,
			Mensagem: "polígono inválido: são necessárias no mínimo 3 coordenadas para formar uma área fechada válida",
		})
		return
	}

	// Validação das coordenadas geográficas
	for i, p := range req.Pontos {
		if err := geo.ValidarCoordenada(p.Latitude, p.Longitude); err != nil {
			escreverJSON(w, http.StatusBadRequest, ConquerPolygonResponse{
				Sucesso:  false,
				Mensagem: fmt.Sprintf("ponto %d inválido (lat=%.6f, lng=%.6f): %v", i+1, p.Latitude, p.Longitude, err),
			})
			return
		}
	}

	areaM2, _, err := h.svc.ConquistarPoligono(r.Context(), usuarioID, req)
	if err != nil {
		h.log.ErrorContext(r.Context(), "erro ao processar conquista de poligono", "erro", err)
		escreverJSON(w, http.StatusInternalServerError, ConquerPolygonResponse{
			Sucesso:  false,
			Mensagem: "erro interno ao processar conquista de território",
		})
		return
	}

	escreverJSON(w, http.StatusOK, ConquerPolygonResponse{
		Sucesso:  true,
		Mensagem: fmt.Sprintf("Território de %.2f m² conquistado com sucesso!", areaM2),
		AreaM2:   areaM2,
	})
}

// ConquistarPoligono é um alias para ConquistarPoligonoHandler.
func (h *Handler) ConquistarPoligono(w http.ResponseWriter, r *http.Request) {
	h.ConquistarPoligonoHandler(w, r)
}

// Visiveis trata GET /api/territorios/visiveis.
//
// Aceita:
//
//	?min_lat=&min_lng=&max_lat=&max_lng=   (viewport do mapa)
//	?lat=&lng=&raio=                       (centro + raio em metros)
func (h *Handler) Visiveis(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var (
		area AreaConsulta
		err  error
	)
	switch {
	case q.Has("min_lat") || q.Has("max_lat") || q.Has("min_lng") || q.Has("max_lng"):
		area.MinLat, err = parseFloat(q.Get("min_lat"), "min_lat", err)
		area.MinLng, err = parseFloat(q.Get("min_lng"), "min_lng", err)
		area.MaxLat, err = parseFloat(q.Get("max_lat"), "max_lat", err)
		area.MaxLng, err = parseFloat(q.Get("max_lng"), "max_lng", err)
	case q.Has("lat") || q.Has("lng"):
		area.PorRaio = true
		area.Lat, err = parseFloat(q.Get("lat"), "lat", err)
		area.Lng, err = parseFloat(q.Get("lng"), "lng", err)
		area.RaioM, err = parseFloat(q.Get("raio"), "raio", err)
	default:
		err = errors.New("informe min_lat,min_lng,max_lat,max_lng ou lat,lng,raio")
	}
	if err != nil {
		escreverErro(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := h.svc.Visiveis(r.Context(), area)
	if err != nil {
		h.tratarErro(w, r, err)
		return
	}
	escreverJSON(w, http.StatusOK, resp)
}

func (h *Handler) tratarErro(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNaoAutenticado):
		escreverErro(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrAtividadeInativa):
		escreverErro(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrRequisicao):
		escreverErro(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		escreverErro(w, http.StatusServiceUnavailable, "requisição cancelada")
	default:
		h.log.ErrorContext(r.Context(), "erro interno em territorios", "path", r.URL.Path, "erro", err)
		escreverErro(w, http.StatusInternalServerError, "erro interno")
	}
}

// parseFloat acumula o primeiro erro, permitindo encadear várias leituras.
func parseFloat(v, nome string, prev error) (float64, error) {
	if prev != nil {
		return 0, prev
	}
	if v == "" {
		return 0, errors.New("parâmetro obrigatório ausente: " + nome)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, errors.New("parâmetro numérico inválido: " + nome)
	}
	return f, nil
}

func escreverJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func escreverErro(w http.ResponseWriter, status int, msg string) {
	escreverJSON(w, status, map[string]string{"erro": msg})
}
