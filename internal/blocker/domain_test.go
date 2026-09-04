package blocker

import "testing"

// TestDominioValido cobre a checagem que impede lixo de chegar ao /etc/hosts.
//
// O caso que motivou tudo isso é o da quebra de linha: NormalizarDominio
// remove espaços das pontas, mas uma quebra no meio da string sobrevivia
// intacta e virava uma linha extra no arquivo do sistema.
func TestDominioValido(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  string
		esperado bool
	}{
		{"domínio simples", "exemplo.com", true},
		{"subdomínio", "m.youtube.com", true},
		{"com hífen", "meu-site.com.br", true},
		{"com sublinhado", "_dmarc.exemplo.com", true},
		{"punycode", "xn--80ak6aa92e.com", true},

		{"vazio", "", false},
		{"sem ponto", "localhost", false},
		{"ponto no início", ".exemplo.com", false},
		{"ponto no fim", "exemplo.com.", false},
		{"pontos seguidos", "exemplo..com", false},
		{"hífen no início", "-exemplo.com", false},
		{"hífen no fim", "exemplo.com-", false},

		// Os que importam de verdade: injeção no /etc/hosts.
		{"quebra de linha", "exemplo.com\n0.0.0.0 outra-coisa.com", false},
		{"retorno de carro", "exemplo.com\r0.0.0.0 outra.com", false},
		{"espaço no meio", "exemplo.com 0.0.0.0", false},
		{"comentário", "exemplo.com # nada", false},
		{"tabulação", "exemplo.com\t0.0.0.0", false},
		{"nulo", "exemplo.com\x00", false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := DominioValido(c.entrada); got != c.esperado {
				t.Errorf("DominioValido(%q) = %v, esperado %v", c.entrada, got, c.esperado)
			}
		})
	}
}

// TestNormalizarNaoSalvaDominioInjetado garante que as duas funções, usadas em
// conjunto como no CLI, recusam a injeção. Normalizar sozinho não basta.
func TestNormalizarNaoSalvaDominioInjetado(t *testing.T) {
	injecao := "Exemplo.com\n0.0.0.0 banco.com"

	normalizado := NormalizarDominio(injecao)
	if DominioValido(normalizado) {
		t.Fatalf("normalizar+validar aceitou uma injeção: %q", normalizado)
	}
}
