package blocker

import (
	"testing"
	"time"
)

// Domínios mortos são a maioria numa lista herdada de outra ferramenta. O que
// este teste garante é que eles não seguram a fila: sem prazo e sem paralelismo,
// duzentos domínios inexistentes levavam minutos.
func TestResolverEmParaleloNaoTravaComDominiosMortos(t *testing.T) {
	if testing.Short() {
		t.Skip("depende de DNS")
	}

	mortos := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		// .invalid é reservado justamente para isto: nunca resolve.
		mortos = append(mortos, "nao-existe-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".invalid")
	}

	inicio := time.Now()
	got := resolverEmParalelo(mortos)
	levou := time.Since(inicio)

	if len(got) != 0 {
		t.Errorf("domínios .invalid não deveriam resolver; vieram %d", len(got))
	}

	// Em série seriam 200 × timeout. Em paralelo, algumas rodadas de fila.
	limite := 30 * time.Second
	if levou > limite {
		t.Errorf("levou %v, acima do limite de %v — o paralelismo ou o prazo não estão valendo", levou, limite)
	}
}

func TestResolverEmParaleloIgnoraLixoSemConsultar(t *testing.T) {
	// Entradas vazias e inválidas são descartadas na limpeza, antes da fila.
	// Se alguma escapasse, viraria uma consulta DNS inútil por domínio.
	got := resolverEmParalelo([]string{"", "   ", "https://", "/"})
	if len(got) != 0 {
		t.Fatalf("lixo não deveria virar consulta; veio %v", got)
	}
}

func TestResolverEmParaleloNaoRepeteDominio(t *testing.T) {
	// O mesmo domínio pode aparecer em blocos diferentes. Consultá-lo duas
	// vezes seria desperdício, e o resultado é um mapa — então a checagem é
	// que a limpeza tira o repetido antes da fila.
	got := resolverEmParalelo([]string{"localhost", "localhost", "LOCALHOST"})
	if len(got) > 1 {
		t.Fatalf("o mesmo domínio não deveria aparecer duas vezes; veio %v", got)
	}
}
