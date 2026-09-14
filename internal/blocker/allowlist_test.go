package blocker

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizarPermitidoPreservaCaminho(t *testing.T) {
	// Este é o ponto inteiro da função: NormalizarDominio cortaria o caminho e
	// "google.fr/maps" liberaria o Google inteiro.
	if got := NormalizarPermitido("google.fr/maps"); got != "google.fr/maps" {
		t.Fatalf("got %q, queria google.fr/maps", got)
	}
}

func TestNormalizarPermitidoTraduzSintaxeDeOutrasFerramentas(t *testing.T) {
	casos := map[string]string{
		"file://*.*":        "file://*",
		"https://wise.com/": "wise.com",
		"  EUROSTAR.COM  ":  "eurostar.com",
		"localhost":         "localhost",
		"127.0.0.1":         "127.0.0.1",
		"devdocs.io\r":      "devdocs.io", // listas exportadas vêm com CRLF
	}
	for entrada, espera := range casos {
		if got := NormalizarPermitido(entrada); got != espera {
			t.Errorf("NormalizarPermitido(%q) = %q, queria %q", entrada, got, espera)
		}
	}
}

func TestGerarPermitidosChromiumNaoUsaCuringas(t *testing.T) {
	// O Chromium descarta filtros com curinga em silêncio. Um "*://*.x/*" aqui
	// não daria erro — simplesmente não liberaria nada, e o modo lista-branca
	// viraria um bloqueio total sem exceções.
	for _, f := range GerarPermitidosChromium([]string{"wise.com", "google.fr/maps"}) {
		if strings.Contains(f, "*") && f != "file://*" {
			t.Fatalf("filtro do Chromium não pode ter curinga: %q", f)
		}
	}
}

func TestGerarExcecoesFirefoxCobreSubdominiosEcaminho(t *testing.T) {
	got := GerarExcecoesFirefox([]string{"gouv.fr", "google.fr/maps"})

	quer := []string{
		"*://*.google.fr/maps*",
		"*://*.gouv.fr/*",
		"*://google.fr/maps*",
		"*://gouv.fr/*",
	}
	for _, q := range quer {
		if !slices.Contains(got, q) {
			t.Errorf("faltou %q em %v", q, got)
		}
	}
}

// O caso perigoso: uma entrada genérica na lista-branca anulando um bloco
// permanente. No Chromium a permissão vence o bloqueio, então isto tem de ser
// resolvido antes de escrever o arquivo.
func TestSubtrairBloqueadosProtegeBlocosPermanentes(t *testing.T) {
	permitidos := []string{"wise.com", "exemplo.com", "jogos.exemplo.com"}
	bloqueados := []string{"exemplo.com"}

	got := SubtrairBloqueados(permitidos, bloqueados)

	if slices.Contains(got, "exemplo.com") {
		t.Error("domínio bloqueado não pode sobreviver na lista-branca")
	}
	if slices.Contains(got, "jogos.exemplo.com") {
		t.Error("subdomínio de domínio bloqueado também não pode sobreviver")
	}
	if !slices.Contains(got, "wise.com") {
		t.Error("domínio sem conflito deveria continuar permitido")
	}
}

func TestSubtrairBloqueadosSemBloqueiosNaoMexe(t *testing.T) {
	permitidos := []string{"wise.com", "gouv.fr"}
	if got := SubtrairBloqueados(permitidos, nil); len(got) != 2 {
		t.Fatalf("sem bloqueios, a lista deveria passar intacta; veio %v", got)
	}
}

// A política montada para os dois navegadores tem de dizer a mesma coisa, cada
// uma na sua sintaxe. Se divergirem, o daemon reescreve os arquivos a cada 5s.
func TestMontarPoliticaListaBranca(t *testing.T) {
	ff, ffExc, ch, chPerm := montarPolitica(Politica{
		Permitidos:  []string{"wise.com"},
		Bloqueados:  []string{"reddit.com"},
		ListaBranca: true,
	})

	if len(ff) != 1 || ff[0] != TodasAsURLsFirefox {
		t.Fatalf("Firefox deveria bloquear tudo; veio %v", ff)
	}
	if len(ch) != 1 || ch[0] != TodasAsURLsChromium {
		t.Fatalf("Chromium deveria bloquear tudo; veio %v", ch)
	}
	if len(ffExc) == 0 || len(chPerm) == 0 {
		t.Fatal("as exceções não podem ficar vazias, senão nada passa")
	}
	if !slices.Contains(chPerm, "wise.com") {
		t.Errorf("wise.com deveria estar liberado; veio %v", chPerm)
	}
}

func TestMontarPoliticaModoNormalNaoGeraPermissoes(t *testing.T) {
	_, ffExc, _, chPerm := montarPolitica(Politica{Bloqueados: []string{"reddit.com"}})

	if len(ffExc) != 0 || len(chPerm) != 0 {
		t.Fatalf("modo normal não deveria gerar exceções; veio %v / %v", ffExc, chPerm)
	}
}
