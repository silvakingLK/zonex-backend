package geo

import (
	"errors"
	"math"
	"testing"
)

// Valor de referência da documentação oficial do H3/h3-go.
const (
	sfLat   = 37.775938728915946
	sfLng   = -122.41795063018799
	sfHexR9 = "8928308280fffff"
)

// Avenida Paulista (São Paulo) — usada como ponto de teste "local".
const (
	spLat = -23.561414
	spLng = -46.655881
)

func TestParaHex_ValorDeReferencia(t *testing.T) {
	got, err := ParaHex(sfLat, sfLng, ResolucaoPadrao)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != sfHexR9 {
		t.Fatalf("ParaHex = %q, esperado %q", got, sfHexR9)
	}
}

func TestParaHex_ResolucaoEDeterminismo(t *testing.T) {
	a, err := ParaHex(spLat, spLng, ResolucaoPadrao)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ParaHex(spLat, spLng, ResolucaoPadrao)
	if a != b {
		t.Fatalf("conversão não determinística: %s != %s", a, b)
	}
	res, err := Resolucao(a)
	if err != nil || res != ResolucaoPadrao {
		t.Fatalf("Resolucao(%s) = %d, %v; esperado %d", a, res, err, ResolucaoPadrao)
	}
	// Um deslocamento de ~1m deve, na imensa maioria dos casos, cair na mesma célula.
	c, _ := ParaHex(spLat+0.00001, spLng, ResolucaoPadrao)
	if a != c {
		t.Logf("aviso: ponto deslocado caiu em célula vizinha (borda): %s vs %s", a, c)
	}
	// ~2km de distância obrigatoriamente é outra célula na res 9.
	d, _ := ParaHex(spLat+0.02, spLng, ResolucaoPadrao)
	if a == d {
		t.Fatalf("pontos a ~2km não deveriam compartilhar célula res 9")
	}
}

func TestParaHex_Invalidos(t *testing.T) {
	casos := []struct {
		nome     string
		lat, lng float64
		res      int
		errAlvo  error
	}{
		{"lat > 90", 91, 0, 9, ErrCoordenadaInvalida},
		{"lat < -90", -90.5, 0, 9, ErrCoordenadaInvalida},
		{"lng > 180", 0, 181, 9, ErrCoordenadaInvalida},
		{"NaN", math.NaN(), 0, 9, ErrCoordenadaInvalida},
		{"Inf", 0, math.Inf(1), 9, ErrCoordenadaInvalida},
		{"res negativa", 0, 0, -1, ErrResolucaoInvalida},
		{"res > 15", 0, 0, 16, ErrResolucaoInvalida},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := ParaHex(c.lat, c.lng, c.res)
			if !errors.Is(err, c.errAlvo) {
				t.Fatalf("erro = %v, esperado %v", err, c.errAlvo)
			}
		})
	}
}

func TestPoligono(t *testing.T) {
	hex, _ := ParaHex(spLat, spLng, ResolucaoPadrao)
	vertices, err := Poligono(hex)
	if err != nil {
		t.Fatal(err)
	}
	if len(vertices) != 6 {
		t.Fatalf("hexágono deveria ter 6 vértices, tem %d", len(vertices))
	}
	centro, _ := Centro(hex)
	for i, v := range vertices {
		// Cada vértice deve estar a < 300m do centro na resolução 9.
		dLat := (v.Latitude - centro.Latitude) * metrosPorGrauLat
		dLng := (v.Longitude - centro.Longitude) * metrosPorGrauLat * math.Cos(centro.Latitude*math.Pi/180)
		if dist := math.Hypot(dLat, dLng); dist > 300 || dist < 50 {
			t.Fatalf("vértice %d a %.1fm do centro (fora do esperado)", i, dist)
		}
	}
	// O ponto original deve converter de volta para a mesma célula do centro.
	if back, _ := ParaHex(centro.Latitude, centro.Longitude, ResolucaoPadrao); back != hex {
		t.Fatalf("centro da célula converteu para %s, esperado %s", back, hex)
	}
}

func TestPoligono_HexInvalido(t *testing.T) {
	for _, id := range []string{"", "abc", "zzzzzzzzzzzzzzz", "0"} {
		if _, err := Poligono(id); !errors.Is(err, ErrHexInvalido) {
			t.Errorf("Poligono(%q) erro = %v, esperado ErrHexInvalido", id, err)
		}
		if HexValido(id) {
			t.Errorf("HexValido(%q) = true", id)
		}
	}
}

func TestCelulasNoRetangulo(t *testing.T) {
	// ~1km x ~1km em torno da Paulista.
	d := 0.0045
	celulas, err := CelulasNoRetangulo(spLat-d, spLng-d, spLat+d, spLng+d, ResolucaoPadrao, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(celulas) < 10 {
		t.Fatalf("esperava várias células, obteve %d", len(celulas))
	}
	centro, _ := ParaHex(spLat, spLng, ResolucaoPadrao)
	if !contem(celulas, centro) {
		t.Fatalf("célula central %s ausente do resultado", centro)
	}
	// As células dos cantos (parcialmente visíveis) devem estar incluídas graças ao buffer.
	for _, canto := range [][2]float64{{spLat - d, spLng - d}, {spLat + d, spLng + d}} {
		h, _ := ParaHex(canto[0], canto[1], ResolucaoPadrao)
		if !contem(celulas, h) {
			t.Errorf("célula do canto %v (%s) ausente", canto, h)
		}
	}
	if dup := duplicado(celulas); dup != "" {
		t.Fatalf("célula duplicada: %s", dup)
	}
}

func TestCelulasNoRetangulo_Erros(t *testing.T) {
	if _, err := CelulasNoRetangulo(1, 1, 0, 2, 9, 100); !errors.Is(err, ErrAreaInvalida) {
		t.Errorf("min>max: erro = %v", err)
	}
	if _, err := CelulasNoRetangulo(-23.8, -46.9, -23.3, -46.3, 9, 1000); !errors.Is(err, ErrAreaMuitoGrande) {
		t.Errorf("área gigante: erro = %v", err)
	}
	if _, err := CelulasNoRetangulo(-95, 0, 0, 1, 9, 100); !errors.Is(err, ErrCoordenadaInvalida) {
		t.Errorf("coord inválida: erro = %v", err)
	}
}

func TestCelulasNoRaio(t *testing.T) {
	celulas, err := CelulasNoRaio(spLat, spLng, 500, ResolucaoPadrao, 500)
	if err != nil {
		t.Fatal(err)
	}
	centro, _ := ParaHex(spLat, spLng, ResolucaoPadrao)
	if !contem(celulas, centro) {
		t.Fatal("célula central ausente")
	}
	// Círculo de 500m (~0,785 km²) / ~0,105 km² por célula ≈ 7,5 células;
	// com as bordas parcialmente cobertas o total fica entre ~7 e ~60.
	if len(celulas) < 7 || len(celulas) > 60 {
		t.Fatalf("quantidade inesperada de células: %d", len(celulas))
	}
	if _, err := CelulasNoRaio(spLat, spLng, 50_000, ResolucaoPadrao, 1000); !errors.Is(err, ErrAreaMuitoGrande) {
		t.Errorf("raio gigante: erro = %v", err)
	}
	if _, err := CelulasNoRaio(spLat, spLng, 0, ResolucaoPadrao, 1000); !errors.Is(err, ErrAreaInvalida) {
		t.Errorf("raio zero: erro = %v", err)
	}
}

func contem(lista []string, alvo string) bool {
	for _, s := range lista {
		if s == alvo {
			return true
		}
	}
	return false
}

func duplicado(lista []string) string {
	vistos := make(map[string]struct{}, len(lista))
	for _, s := range lista {
		if _, ok := vistos[s]; ok {
			return s
		}
		vistos[s] = struct{}{}
	}
	return ""
}

func TestCalcularAreaPoligonoM2(t *testing.T) {
	// Retângulo em São Paulo:
	// Altura: 0.001 grau de latitude ≈ 111.32 metros
	// Largura: 0.001 grau de longitude ≈ 111.32 * cos(-23.56°) ≈ 102.04 metros
	// Área aproximada: 111.32 * 102.04 ≈ 11,360 m²
	coords := []Coordenada{
		{Latitude: spLat, Longitude: spLng},
		{Latitude: spLat, Longitude: spLng + 0.001},
		{Latitude: spLat + 0.001, Longitude: spLng + 0.001},
		{Latitude: spLat + 0.001, Longitude: spLng},
	}
	area := CalcularAreaPoligonoM2(coords)
	if area < 11000 || area > 11800 {
		t.Fatalf("área calculada fora do esperado: %.2f m²", area)
	}

	// Com ponto de fechamento repetido (anel fechado) o resultado deve ser idêntico
	coordsFechadas := append(coords, coords[0])
	areaFechada := CalcularAreaPoligonoM2(coordsFechadas)
	if math.Abs(area-areaFechada) > 0.001 {
		t.Fatalf("área de anel fechado diverge: %.2f vs %.2f", area, areaFechada)
	}

	// Menos de 3 pontos retorna 0
	if a := CalcularAreaPoligonoM2(coords[:2]); a != 0 {
		t.Fatalf("esperava 0 para menos de 3 pontos, obteve %f", a)
	}
}

func TestCelulasNoPoligono(t *testing.T) {
	coords := []Coordenada{
		{Latitude: spLat, Longitude: spLng},
		{Latitude: spLat, Longitude: spLng + 0.005},
		{Latitude: spLat + 0.005, Longitude: spLng + 0.005},
		{Latitude: spLat + 0.005, Longitude: spLng},
	}
	celulas := CelulasNoPoligono(coords, ResolucaoPadrao)
	if len(celulas) == 0 {
		t.Fatal("esperava células conquistadas pelo polígono")
	}
}

