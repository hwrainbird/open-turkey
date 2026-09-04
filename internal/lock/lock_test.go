package lock

import "testing"

// TestSanitizeChars cobre os dois erros que a versão anterior permitia:
// um desafio vazio (destravável com um Enter) e um valor negativo, que
// derrubava o programa e deixava o bloco sem forma de ser desativado.
func TestSanitizeChars(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  int
		esperado int
	}{
		{"zero vira o padrão", 0, DefaultLockChars},
		{"negativo vira o padrão", -1, DefaultLockChars},
		{"abaixo do mínimo vira o padrão", MinLockChars - 1, DefaultLockChars},
		{"no mínimo é aceito", MinLockChars, MinLockChars},
		{"padrão é aceito", DefaultLockChars, DefaultLockChars},
		{"no máximo é aceito", MaxLockChars, MaxLockChars},
		{"acima do máximo é limitado", MaxLockChars + 1, MaxLockChars},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := SanitizeChars(c.entrada); got != c.esperado {
				t.Errorf("SanitizeChars(%d) = %d, esperado %d", c.entrada, got, c.esperado)
			}
		})
	}
}

// TestSanitizeCharsNuncaEnfraquece é a propriedade que realmente importa:
// nenhum valor de entrada pode produzir um desafio mais curto que o mínimo.
func TestSanitizeCharsNuncaEnfraquece(t *testing.T) {
	for _, n := range []int{-1000, -1, 0, 1, 49, 50, 300, 5000, 10000} {
		if got := SanitizeChars(n); got < MinLockChars {
			t.Errorf("SanitizeChars(%d) = %d, abaixo do mínimo %d", n, got, MinLockChars)
		}
	}
}

// TestDividirEmLinhas confere que nenhum caractere se perde ou se duplica ao
// quebrar o desafio em pedaços — a pessoa precisa digitar exatamente o que foi
// gerado, nem mais nem menos.
func TestDividirEmLinhas(t *testing.T) {
	casos := []struct {
		nome     string
		texto    string
		largura  int
		esperado []string
	}{
		{"exato", "abcdef", 3, []string{"abc", "def"}},
		{"com sobra", "abcdefg", 3, []string{"abc", "def", "g"}},
		{"menor que a largura", "ab", 10, []string{"ab"}},
		{"largura inválida", "abc", 0, []string{"abc"}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := dividirEmLinhas(c.texto, c.largura)
			if len(got) != len(c.esperado) {
				t.Fatalf("dividirEmLinhas(%q, %d) = %v, esperado %v", c.texto, c.largura, got, c.esperado)
			}
			juntado := ""
			for i, linha := range got {
				if linha != c.esperado[i] {
					t.Errorf("linha %d = %q, esperado %q", i, linha, c.esperado[i])
				}
				juntado += linha
			}
			if juntado != c.texto {
				t.Errorf("juntar as linhas devolveu %q, esperado %q", juntado, c.texto)
			}
		})
	}
}

// TestGenerateChallengeUsaSomenteOCharset confere que o desafio nunca contém
// um caractere impossível de digitar.
func TestGenerateChallengeUsaSomenteOCharset(t *testing.T) {
	desafio, err := GenerateChallenge(500)
	if err != nil {
		t.Fatalf("GenerateChallenge devolveu erro: %v", err)
	}
	if len(desafio) != 500 {
		t.Fatalf("GenerateChallenge(500) devolveu %d caracteres", len(desafio))
	}
	for i := 0; i < len(desafio); i++ {
		found := false
		for j := 0; j < len(charset); j++ {
			if desafio[i] == charset[j] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("caractere fora do charset na posição %d: %q", i, desafio[i])
		}
	}
}
