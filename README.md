# ZoneX Backend — Motor de Conquista Territorial (H3)

Disputa de células hexagonais H3 (resolução 9, ~174 m de aresta) durante atividades físicas.

## Estrutura

```
cmd/api/main.go                         # servidor HTTP (chi), graceful shutdown
internal/geo/hex.go                     # wrapper H3: coord→célula, polígono, área→células
internal/territorio/territorio.go       # modelo, regras puras de disputa, interface Repositorio
internal/territorio/servico.go          # orquestração (validação, atividade ativa, conquista)
internal/territorio/handler.go          # endpoints HTTP + middleware de auth (dev)
internal/territorio/repositorio_postgres.go
internal/territorio/repositorio_memoria.go
migrations/                             # SQL (formato golang-migrate)
```

## Regras de disputa

| Situação da célula            | Resultado     | Efeito                                        |
|-------------------------------|---------------|-----------------------------------------------|
| Neutra (inexistente/sem dono) | `conquistado` | dono = você, defesa = 1, `dominado_em` = agora |
| Sua                           | `reforcado`   | defesa +1 (até `MaxDefesa`, padrão 5)         |
| Inimiga, defesa > 1           | `atacado`     | defesa −1, dono mantido                       |
| Inimiga, defesa = 1           | `tomado`      | dono = você, defesa = 1, `dominado_em` = agora |

A conquista roda numa transação com `pg_advisory_xact_lock` por célula + `SELECT … FOR UPDATE`
+ `INSERT … ON CONFLICT DO UPDATE`: disputas simultâneas na mesma célula são serializadas sem perda de atualização.

## API

Autenticação (dev): header `X-Usuario-ID`. **Substitua** `MiddlewareUsuarioHeaderDev` pelo middleware JWT,
chamando `territorio.ComUsuario(ctx, userID)`.

### `GET /api/territorios/visiveis`
- Viewport: `?min_lat=-23.565&min_lng=-46.66&max_lat=-23.557&max_lng=-46.65`
- Centro + raio (m): `?lat=-23.5614&lng=-46.6558&raio=500`
- Limite de 2500 células por consulta (400 se exceder → app deve aproximar o zoom).

```json
{ "resolucao": 9, "total": 42, "dominados": 1,
  "hexagonos": [{ "hex_id": "89a8100c2d3ffff", "status": "dominado", "dono_id": 42,
                  "pontos_defesa": 2, "dominado_em": "2026-10-14T21:00:00Z",
                  "poligono": [{ "latitude": -23.56, "longitude": -46.65 }, "..."] }] }
```
`poligono` usa o formato do `<Polygon coordinates>` do react-native-maps.

### `POST /api/territorios/conquistar`
```json
{ "latitude": -23.561414, "longitude": -46.655881, "atividade_id": 10 }
```
Resposta `200`: `hex_id`, `status`, `dono_id`, `dono_anterior_id`, `pontos_defesa`, `dominado_em`, `poligono`.
Erros: `400` payload/coordenada inválidos · `401` sem usuário · `403` atividade inexistente/de outro usuário/não `em_andamento`.

## Rodando

> **H3 exige cgo.** No Windows é preciso um GCC **64-bit** (ex.: MSYS2 `mingw-w64-ucrt-x86_64-gcc`, ou TDM-GCC 64).
> O MinGW 32-bit clássico falha com `64-bit mode not compiled in`. Em Linux/Docker basta `gcc`/`build-essential`.

```bash
docker compose up -d                     # PostGIS + migrations
export DATABASE_URL="postgres://zonex:zonex@localhost:5432/zonex?sslmode=disable"
go run ./cmd/api

# sem banco (dados em memória; usuário 1 com atividade 1 ativa):
DATABASE_URL=memoria go run ./cmd/api

go test ./...
```
