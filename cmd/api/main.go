// Comando api sobe o servidor HTTP do ZoneX.
//
// Variáveis de ambiente:
//
//	DATABASE_URL  postgres://user:pass@host:5432/zonex?sslmode=disable
//	              (use "memoria" para rodar sem banco, apenas para desenvolvimento)
//	PORT          porta HTTP (padrão 8080)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zonex/backend/internal/territorio"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("encerrando com erro", "erro", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, fechar, err := abrirRepositorio(ctx, log)
	if err != nil {
		return err
	}
	defer fechar()

	svc := territorio.NovoServico(repo, territorio.Config{})
	h := territorio.NovoHandler(svc, log)

	r := chi.NewRouter()
	r.Use(middlewareCORS)
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, middleware.Timeout(10*time.Second))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Route("/api", func(api chi.Router) {
		// TODO: substituir pelo middleware JWT do ZoneX (deve chamar territorio.ComUsuario).
		api.Use(territorio.MiddlewareUsuarioHeaderDev)
		api.Route("/territorios", h.Rotas)
	})


	porta := os.Getenv("PORT")
	if porta == "" {
		porta = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + porta,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	erros := make(chan error, 1)
	go func() {
		log.Info("ZoneX API ouvindo", "porta", porta)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
		close(erros)
	}()

	select {
	case err := <-erros:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func abrirRepositorio(ctx context.Context, log *slog.Logger) (territorio.Repositorio, func(), error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "memoria" {
		log.Warn("usando repositório EM MEMÓRIA (dados não persistem); atividade 1 do usuário 1 iniciada para testes")
		m := territorio.NovoRepositorioMemoria()
		m.IniciarAtividade(1, 1)
		return m, func() {}, nil
	}
	if dsn == "" {
		return nil, nil, errors.New("DATABASE_URL não definida (use DATABASE_URL=memoria para desenvolvimento)")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, err
	}
	return territorio.NovoRepositorioPostgres(pool), pool.Close, nil
}

// middlewareCORS configura os headers CORS para permitir que clientes web e apps mobile
// acessem a API com os headers customizados necessários (ex: Content-Type, X-Usuario-ID).
func middlewareCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Usuario-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Link")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

