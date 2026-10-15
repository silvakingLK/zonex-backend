package territorio

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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
