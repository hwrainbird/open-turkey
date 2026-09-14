// Modo lista-branca: bloquear tudo e abrir exceção para uma lista curta.
//
// === POR QUE SÓ NO NAVEGADOR ===
//
// "Bloqueie tudo menos estes sites" não tem como ser feito no /etc/hosts: não
// dá para enumerar a internet inteira para apontá-la ao 0.0.0.0.
//
// No firewall daria — bastaria negar tudo por padrão —, mas seria uma péssima
// ideia. Os endereços IP dos sites permitidos mudam o tempo todo (é o normal
// em qualquer CDN), então um site liberado quebraria sozinho no meio da
// semana. Pior: numa máquina em que você abriu mão do acesso de administrador
// de propósito, uma regra de firewall malfeita é uma máquina sem rede e sem
// como consertar.
//
// As políticas de navegador fazem exatamente isso de forma nativa e segura:
// bloqueia-se tudo e listam-se as exceções. O terminal, o ssh, o git e as
// atualizações do sistema continuam intocados — eles não leem política de
// navegador. E é no navegador que a distração mora.
//
// === AS DUAS SINTAXES ===
//
// Firefox (WebsiteFilter): Block aceita "<all_urls>" e Exceptions aceita
// match-patterns no estilo "*://*.dominio.com/*".
//
// Chromium/Chrome/Brave: URLBlocklist aceita "*" e URLAllowlist aceita o
// formato "[scheme://][.]host[:port][/path]". A lista-branca vence a
// lista-negra — é essa precedência que faz o modo funcionar.
package blocker

import (
	"sort"
	"strings"
)

// TodasAsURLsFirefox é o padrão que o Firefox entende como "tudo".
const TodasAsURLsFirefox = "<all_urls>"

// TodasAsURLsChromium é o equivalente no Chromium.
const TodasAsURLsChromium = "*"

// Politica descreve o que os navegadores devem impedir e o que devem deixar
// passar num dado momento.
//
// ListaBranca inverte o sentido de tudo: em vez de bloquear os domínios de
// Bloqueados, bloqueia-se a internet inteira e abre-se exceção para
// Permitidos.
type Politica struct {
	Bloqueados  []string
	Permitidos  []string
	ListaBranca bool
}

// NormalizarPermitido limpa uma entrada da lista-branca preservando o caminho.
//
// NormalizarDominio não serve aqui porque ela corta o caminho: "google.fr/maps"
// viraria "google.fr" e liberaria o Google inteiro, que é o oposto do que a
// entrada pede.
//
// Entradas que não são domínio (o "localhost" e o "127.0.0.1" que aparecem em
// lista exportada de outras ferramentas) passam intactas — são endereços
// válidos para o navegador, mesmo não sendo nomes de domínio.
func NormalizarPermitido(entrada string) string {
	s := strings.TrimSpace(strings.ToLower(entrada))
	s = strings.TrimSuffix(s, "\r") // listas exportadas costumam vir com CRLF
	if s == "" {
		return ""
	}

	// "file://*.*" é sintaxe do Cold Turkey. O equivalente aceito pelos dois
	// navegadores é simplesmente o esquema file.
	if strings.HasPrefix(s, "file://") {
		return "file://*"
	}

	// Tiramos o esquema, se houver: o padrão que geramos cobre http e https.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}

	return strings.TrimSuffix(s, "/")
}

// GerarExcecoesFirefox converte a lista-branca em match-patterns do Firefox.
//
// Uma entrada com caminho vira um padrão com caminho: "google.fr/maps" produz
// "*://*.google.fr/maps*", que libera o Maps sem liberar a busca.
func GerarExcecoesFirefox(entradas []string) []string {
	conjunto := map[string]bool{}

	for _, bruta := range entradas {
		e := NormalizarPermitido(bruta)
		if e == "" {
			continue
		}
		if e == "file://*" {
			conjunto[e] = true
			continue
		}

		host, caminho, temCaminho := strings.Cut(e, "/")
		host = removerPrefixoWWW(host)
		if host == "" {
			continue
		}

		if temCaminho && caminho != "" {
			conjunto["*://*."+host+"/"+caminho+"*"] = true
			conjunto["*://"+host+"/"+caminho+"*"] = true
		} else {
			conjunto["*://*."+host+"/*"] = true
			conjunto["*://"+host+"/*"] = true
		}
	}

	return ordenado(conjunto)
}

// GerarPermitidosChromium converte a lista-branca para o formato do Chromium.
//
// Aqui o formato é mais simples que o do Firefox: "dominio.com" já casa o
// domínio, todos os seus subdomínios e todos os caminhos. Curingas no estilo
// "*://*.host/*" seriam descartados em silêncio — o mesmo erro que já causou
// bug em produção neste projeto com a lista de bloqueio.
func GerarPermitidosChromium(entradas []string) []string {
	conjunto := map[string]bool{}

	for _, bruta := range entradas {
		e := NormalizarPermitido(bruta)
		if e == "" {
			continue
		}
		if e == "file://*" {
			conjunto["file://*"] = true
			continue
		}
		conjunto[removerPrefixoWWW(e)] = true
	}

	return ordenado(conjunto)
}

// SubtrairBloqueados tira da lista-branca tudo que está bloqueado de propósito.
//
// No Chromium a lista-branca vence a lista-negra. Sem esta subtração, um
// domínio que aparecesse nas duas listas ficaria LIBERADO — e o jeito mais
// provável de isso acontecer é exatamente o pior: um bloco permanente
// (pornografia, apostas) anulado por uma entrada genérica da lista-branca.
//
// A comparação é por sufixo de domínio, porque a lista-branca do Chromium
// cobre subdomínios: liberar "exemplo.com" liberaria "jogos.exemplo.com".
func SubtrairBloqueados(permitidos, bloqueados []string) []string {
	if len(bloqueados) == 0 {
		return permitidos
	}

	proibidos := make([]string, 0, len(bloqueados))
	for _, b := range bloqueados {
		if d := NormalizarDominio(b); d != "" {
			proibidos = append(proibidos, removerPrefixoWWW(d))
		}
	}

	var mantidos []string
	for _, p := range permitidos {
		e := NormalizarPermitido(p)
		if e == "" {
			continue
		}
		host, _, _ := strings.Cut(e, "/")
		host = removerPrefixoWWW(host)

		conflita := false
		for _, b := range proibidos {
			if host == b || strings.HasSuffix(host, "."+b) {
				conflita = true
				break
			}
		}
		if !conflita {
			mantidos = append(mantidos, p)
		}
	}

	return mantidos
}

func ordenado(conjunto map[string]bool) []string {
	saida := make([]string, 0, len(conjunto))
	for k := range conjunto {
		saida = append(saida, k)
	}
	sort.Strings(saida)
	return saida
}
