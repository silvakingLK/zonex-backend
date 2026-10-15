// Package geo concentra as operações geoespaciais do ZoneX baseadas no
// sistema de indexação hexagonal H3 (Uber).
//
// Toda a lógica que conhece H3 fica isolada aqui; o restante do backend
// trabalha apenas com IDs de célula em formato string (ex: "89a8100c2d3ffff").
package geo

import (
	"errors"
	"fmt"
	"math"

	"github.com/uber/h3-go/v3"
)

// ResolucaoPadrao é a resolução H3 usada no jogo.
// Resolução 9 => aresta média de ~174m, área média de ~0,105 km².
const ResolucaoPadrao = 9

const (
	resolucaoMin = 0
	resolucaoMax = 15
	// metrosPorGrauLat é a distância média de 1 grau de latitude.
	metrosPorGrauLat = 111_320.0
)

var (
	ErrCoordenadaInvalida = errors.New("coordenada inválida")
	ErrResolucaoInvalida  = errors.New("resolução H3 inválida")
	ErrHexInvalido        = errors.New("hex_id inválido")
	ErrAreaInvalida       = errors.New("área de busca inválida")
	ErrAreaMuitoGrande    = errors.New("área de busca muito grande")
)

// Coordenada usa as mesmas chaves que o react-native-maps espera em
// <Polygon coordinates={...}/>, evitando conversões no app.
type Coordenada struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ValidarCoordenada garante que lat/lng são números finitos dentro dos limites WGS84.
func ValidarCoordenada(lat, lng float64) error {
	if math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) {
		return ErrCoordenadaInvalida
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return fmt.Errorf("%w: lat=%f lng=%f", ErrCoordenadaInvalida, lat, lng)
	}
	return nil
}

func validarResolucao(res int) error {
	if res < resolucaoMin || res > resolucaoMax {
		return fmt.Errorf("%w: %d", ErrResolucaoInvalida, res)
	}
	return nil
}

// ParaHex converte uma coordenada GPS no ID (string) da célula H3 que a contém.
func ParaHex(lat, lng float64, res int) (string, error) {
	if err := ValidarCoordenada(lat, lng); err != nil {
		return "", err
	}
	if err := validarResolucao(res); err != nil {
		return "", err
	}
	idx := h3.FromGeo(h3.GeoCoord{Latitude: lat, Longitude: lng}, res)
	return h3.ToString(idx), nil
}

// parseHex converte e valida um ID de célula.
func parseHex(hexID string) (h3.H3Index, error) {
	idx := h3.FromString(hexID)
	if idx == 0 || !h3.IsValid(idx) {
		return 0, fmt.Errorf("%w: %q", ErrHexInvalido, hexID)
	}
	return idx, nil
}

// HexValido indica se a string representa uma célula H3 válida.
func HexValido(hexID string) bool {
	_, err := parseHex(hexID)
	return err == nil
}

// Poligono retorna os vértices (6 para hexágonos, 5 para pentágonos) da célula,
// em ordem anti-horária, prontos para desenho no cliente mobile.
func Poligono(hexID string) ([]Coordenada, error) {
	idx, err := parseHex(hexID)
	if err != nil {
		return nil, err
	}
	limite := h3.ToGeoBoundary(idx)
	vertices := make([]Coordenada, len(limite))
	for i, v := range limite {
		vertices[i] = Coordenada{Latitude: v.Latitude, Longitude: v.Longitude}
	}
	return vertices, nil
}

// Centro retorna o centróide da célula.
func Centro(hexID string) (Coordenada, error) {
	idx, err := parseHex(hexID)
	if err != nil {
		return Coordenada{}, err
	}
	c := h3.ToGeo(idx)
	return Coordenada{Latitude: c.Latitude, Longitude: c.Longitude}, nil
}

// Resolucao retorna a resolução de uma célula.
func Resolucao(hexID string) (int, error) {
	idx, err := parseHex(hexID)
	if err != nil {
		return 0, err
	}
	return h3.Resolution(idx), nil
}

// CelulasNoRetangulo retorna todas as células que intersectam o retângulo
// (viewport do mapa). O retângulo é expandido em uma aresta de célula para
// incluir hexágonos parcialmente visíveis nas bordas da tela (o Polyfill do
// H3 considera apenas células cujo centro está dentro do polígono).
//
// limite define o número máximo de células aceito; acima disso retorna
// ErrAreaMuitoGrande (proteção contra requisições abusivas / zoom-out extremo).
// Retângulos que cruzam o antimeridiano (minLng > maxLng) não são suportados.
func CelulasNoRetangulo(minLat, minLng, maxLat, maxLng float64, res, limite int) ([]string, error) {
	if err := ValidarCoordenada(minLat, minLng); err != nil {
		return nil, err
	}
	if err := ValidarCoordenada(maxLat, maxLng); err != nil {
		return nil, err
	}
	if err := validarResolucao(res); err != nil {
		return nil, err
	}
	if minLat >= maxLat || minLng >= maxLng {
		return nil, fmt.Errorf("%w: min deve ser menor que max", ErrAreaInvalida)
	}

	// Buffer de uma aresta (raio circunscrito do hexágono) em graus.
	aresta := h3.EdgeLengthM(res)
	latMedia := (minLat + maxLat) / 2
	bufLat := aresta / metrosPorGrauLat
	bufLng := aresta / (metrosPorGrauLat * math.Max(math.Cos(latMedia*math.Pi/180), 0.01))

	minLat, maxLat = math.Max(minLat-bufLat, -90), math.Min(maxLat+bufLat, 90)
	minLng, maxLng = math.Max(minLng-bufLng, -180), math.Min(maxLng+bufLng, 180)

	// Estimativa barata ANTES do polyfill para não gastar CPU com áreas enormes.
	alturaM := (maxLat - minLat) * metrosPorGrauLat
	larguraM := (maxLng - minLng) * metrosPorGrauLat * math.Cos(latMedia*math.Pi/180)
	estimativa := (alturaM * larguraM) / h3.HexAreaM2(res)
	if limite > 0 && estimativa > float64(limite)*1.2 {
		return nil, fmt.Errorf("%w: ~%.0f células (máx %d)", ErrAreaMuitoGrande, estimativa, limite)
	}

	poligono := h3.GeoPolygon{Geofence: []h3.GeoCoord{
		{Latitude: minLat, Longitude: minLng},
		{Latitude: minLat, Longitude: maxLng},
		{Latitude: maxLat, Longitude: maxLng},
		{Latitude: maxLat, Longitude: minLng},
	}}
	indices := h3.Polyfill(poligono, res)

	if limite > 0 && len(indices) > limite {
		return nil, fmt.Errorf("%w: %d células (máx %d)", ErrAreaMuitoGrande, len(indices), limite)
	}
	return paraStrings(indices), nil
}

// CelulasNoRaio retorna as células cujo hexágono intersecta o círculo
// (centro + raio em metros).
func CelulasNoRaio(lat, lng, raioM float64, res, limite int) ([]string, error) {
	if err := ValidarCoordenada(lat, lng); err != nil {
		return nil, err
	}
	if err := validarResolucao(res); err != nil {
		return nil, err
	}
	if math.IsNaN(raioM) || raioM <= 0 {
		return nil, fmt.Errorf("%w: raio deve ser > 0", ErrAreaInvalida)
	}

	aresta := h3.EdgeLengthM(res)
	// Distância entre centros de hexágonos vizinhos = aresta * √3.
	k := int(math.Ceil((raioM+aresta)/(aresta*math.Sqrt(3)))) + 1
	total := 3*k*(k+1) + 1 // tamanho do k-ring
	if limite > 0 && float64(total) > float64(limite)*1.5 {
		return nil, fmt.Errorf("%w: raio de %.0fm excede o limite de %d células", ErrAreaMuitoGrande, raioM, limite)
	}

	origem := h3.FromGeo(h3.GeoCoord{Latitude: lat, Longitude: lng}, res)
	centro := h3.GeoCoord{Latitude: lat, Longitude: lng}

	candidatos := h3.KRing(origem, k)
	resultado := make([]string, 0, len(candidatos))
	for _, idx := range candidatos {
		// Mantém a célula se algum ponto dela pode estar dentro do círculo:
		// distância até o centro <= raio + raio circunscrito (aresta).
		if h3.PointDistM(centro, h3.ToGeo(idx)) <= raioM+aresta {
			resultado = append(resultado, h3.ToString(idx))
		}
	}
	if limite > 0 && len(resultado) > limite {
		return nil, fmt.Errorf("%w: %d células (máx %d)", ErrAreaMuitoGrande, len(resultado), limite)
	}
	return resultado, nil
}

func paraStrings(indices []h3.H3Index) []string {
	out := make([]string, 0, len(indices))
	for _, idx := range indices {
		if idx == 0 {
			continue
		}
		out = append(out, h3.ToString(idx))
	}
	return out
}
