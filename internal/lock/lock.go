// Package lock implementa o mecanismo de "desafio de digitação" do Open Turkey.
//
// === O QUE É ISSO? ===
//
// Este pacote é a ÚNICA forma de desbloquear um bloqueio ativo no Open Turkey.
// A ideia é simples: criar "fricção". Quando você está tentado a acessar algo
// bloqueado (redes sociais, jogos, etc.), o sistema te obriga a digitar uma
// string aleatória enorme — caractere por caractere, sem erro.
//
// === POR QUE ISSO FUNCIONA? ===
//
// Não é segurança de verdade. Qualquer pessoa com acesso ao código poderia
// burlar isso. O ponto é PSICOLÓGICO: a tarefa chata de digitar centenas de
// caracteres aleatórios te dá tempo para repensar se realmente precisa
// desbloquear. Na maioria das vezes, você desiste — e era isso que queria.
//
// === POR QUE crypto/rand E NÃO math/rand? ===
//
// math/rand gera números "pseudo-aleatórios" — parecem aleatórios, mas seguem
// um padrão previsível se você souber a "semente" (seed). Alguém esperto
// poderia prever a string gerada e colar sem digitar.
//
// crypto/rand usa fontes de aleatoriedade do sistema operacional (como /dev/urandom
// no Linux). É verdadeiramente imprevisível — ninguém consegue adivinhar a
// string antes dela ser gerada. Para o nosso caso, é um exagero? Talvez.
// Mas é a coisa certa a fazer, e o custo é praticamente zero.
package lock

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"time"
)

// DefaultLockChars é a quantidade padrão de caracteres do desafio.
// 300 caracteres é o suficiente para ser bem chato de digitar, mas não
// impossível. Leva uns 3-5 minutos para a maioria das pessoas — tempo
// suficiente para o impulso de "preciso ver o Instagram AGORA" passar.
const DefaultLockChars = 300

// MinLockChars e MaxLockChars delimitam valores aceitáveis para --lock-chars.
//
// O mínimo existe porque um desafio curto não é atrito nenhum: com 0 caracteres
// o desafio virava uma string vazia que qualquer Enter satisfazia, ou seja,
// uma trava que não travava nada.
//
// O máximo existe para evitar o erro oposto — um número absurdo (ou negativo,
// que antes derrubava o programa) que tornaria o bloco impossível de destravar
// e exigiria mexer no banco de dados na mão.
const (
	MinLockChars = 50
	MaxLockChars = 5000
)

// larguraLinha é o tamanho de cada pedaço do desafio.
//
// O desafio é pedido pedaço por pedaço, e não de uma vez só. Isso serve a dois
// propósitos: linhas curtas são mais fáceis de acompanhar com o olho, e não
// existe um momento em que o texto inteiro está na tela para ser selecionado
// e colado de uma vez.
const larguraLinha = 50

// penalidadeErro é a pausa aplicada depois de cada linha digitada errado.
// Não é punição: é para desencorajar tentativa e erro no chute.
const penalidadeErro = 3 * time.Second

// SanitizeChars devolve um tamanho de desafio seguro.
//
// Valores abaixo do mínimo (incluindo 0 e negativos, que podem estar gravados
// em bancos criados antes desta checagem) viram o padrão em vez de virarem um
// desafio trivial. Na dúvida, erramos para o lado de travar mais, nunca menos.
func SanitizeChars(n int) int {
	if n < MinLockChars {
		return DefaultLockChars
	}
	if n > MaxLockChars {
		return MaxLockChars
	}
	return n
}

// charset é o conjunto de caracteres usados para gerar o desafio.
// Inclui letras minúsculas, maiúsculas, números e símbolos.
// A mistura de tipos de caractere torna a digitação mais lenta porque
// você precisa ficar alternando entre Shift, números e letras.
// Isso é intencional — quanto mais difícil, mais fricção.
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"

// GenerateChallenge gera uma string aleatória com o comprimento especificado.
//
// Como funciona, passo a passo:
//  1. Criamos um slice de bytes (pense nisso como uma "lista" de caracteres)
//  2. Para cada posição, usamos crypto/rand.Int() para gerar um número
//     aleatório entre 0 e o tamanho do charset
//  3. Esse número é usado como índice para pegar um caractere do charset
//  4. Repetimos isso 'length' vezes até montar a string completa
//
// Exemplo simplificado:
//
//	charset = "abc" (3 caracteres)
//	rand.Int() devolve 2 → charset[2] = 'c'
//	rand.Int() devolve 0 → charset[0] = 'a'
//	rand.Int() devolve 1 → charset[1] = 'b'
//	Resultado: "cab"
//
// No nosso caso, o charset tem 70 caracteres e o length padrão é 300,
// então o resultado é algo como: "aB3!kZ$m9Q..." (300 caracteres aleatórios).
func GenerateChallenge(length int) (string, error) {
	// big.NewInt converte o tamanho do charset para o tipo que crypto/rand espera.
	// crypto/rand trabalha com números grandes (big.Int) porque foi feito para
	// criptografia, onde os números podem ter centenas de dígitos.
	charsetSize := big.NewInt(int64(len(charset)))

	// Criamos o slice que vai guardar cada caractere gerado.
	// Em Go, um slice é como um array dinâmico — uma lista de tamanho fixo aqui.
	result := make([]byte, length)

	for i := 0; i < length; i++ {
		// rand.Int(rand.Reader, max) gera um número aleatório entre 0 e max-1.
		// rand.Reader é a fonte de aleatoriedade do sistema operacional.
		// Se algo der errado (ex: /dev/urandom não está disponível), retorna erro.
		index, err := rand.Int(rand.Reader, charsetSize)
		if err != nil {
			// Em português: "falha ao gerar número aleatório"
			// Isso praticamente nunca acontece, mas é bom tratar o erro.
			return "", fmt.Errorf("falha ao gerar caractere aleatório na posição %d: %w", i, err)
		}

		// index.Int64() converte o big.Int para um int64 normal,
		// que usamos como índice para pegar o caractere do charset.
		result[i] = charset[index.Int64()]
	}

	// Convertemos o slice de bytes para string e retornamos.
	return string(result), nil
}

// RunChallenge executa o desafio completo de digitação.
//
// === O QUE MUDOU E POR QUÊ ===
//
// A primeira versão mostrava os 300 caracteres de uma vez e lia a resposta da
// entrada padrão. Isso deixava duas saídas abertas:
//
//  1. A entrada padrão pode ser um cano. "echo ... | open-turkey unlock" ou
//     "unlock < arquivo" resolviam o desafio sem ninguém digitar nada.
//  2. Com o texto inteiro na tela, bastava selecionar tudo com o mouse e colar.
//
// Agora lemos direto do terminal (/dev/tty) em vez da entrada padrão, o que
// impede canos e redirecionamentos, e pedimos o desafio uma linha por vez, de
// modo que nunca existe um momento em que o texto todo está disponível para
// ser copiado de uma vez.
//
// Isso não é inviolável e não pretende ser: quem estiver decidido a burlar
// ainda pode colar linha por linha. O objetivo é que burlar exija intenção
// deliberada e sustentada, e não um reflexo de dez segundos.
//
// Retorna true se o desafio foi completado com sucesso.
func RunChallenge(length int) (bool, error) {
	length = SanitizeChars(length)

	challenge, err := GenerateChallenge(length)
	if err != nil {
		return false, fmt.Errorf("falha ao gerar desafio: %w", err)
	}

	// Abrimos o terminal de verdade. Se não houver um, o desafio não acontece —
	// é exatamente esse o ponto.
	tty, err := abrirTerminal()
	if err != nil {
		return false, err
	}
	defer tty.Close()

	linhas := dividirEmLinhas(challenge, larguraLinha)

	fmt.Fprintln(tty)
	fmt.Fprintln(tty, "=== DESAFIO DE DESBLOQUEIO ===")
	fmt.Fprintln(tty)
	fmt.Fprintf(tty, "%d caracteres, em %d linhas.\n", length, len(linhas))
	fmt.Fprintln(tty, "Cada linha só aparece depois que a anterior for digitada corretamente.")
	fmt.Fprintln(tty)

	scanner := bufio.NewScanner(tty)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for i, linha := range linhas {
		// O laço interno só termina quando esta linha sair correta.
		// Errar não recomeça o desafio inteiro — seria punitivo demais para
		// algo que uma pessoa cansada vai digitar de madrugada.
		for {
			fmt.Fprintf(tty, "Linha %d/%d:\n", i+1, len(linhas))
			fmt.Fprintf(tty, "  %s\n", linha)
			fmt.Fprint(tty, "> ")

			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return false, fmt.Errorf("erro ao ler do terminal: %w", err)
				}
				return false, fmt.Errorf("entrada encerrada antes do fim do desafio")
			}

			if scanner.Text() == linha {
				fmt.Fprintln(tty)
				break
			}

			relatarDiferenca(tty, linha, scanner.Text())
			time.Sleep(penalidadeErro)
			fmt.Fprintln(tty)
		}
	}

	fmt.Fprintln(tty, "Desbloqueio autorizado! Desafio concluído com sucesso.")
	return true, nil
}

// abrirTerminal abre /dev/tty, que é sempre o terminal ao qual o processo está
// ligado — mesmo que a entrada padrão tenha sido redirecionada.
//
// É essa diferença que faz o desafio valer alguma coisa. A entrada padrão pode
// vir de um arquivo, de um cano ou de outro programa; /dev/tty só existe se
// houver um terminal de verdade do outro lado. Sem terminal, recusamos.
func abrirTerminal() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf(
			"o desafio de desbloqueio precisa de um terminal de verdade "+
				"(não funciona por cano, redirecionamento ou script): %w", err)
	}
	return tty, nil
}

// dividirEmLinhas quebra o desafio em pedaços de largura fixa.
// O último pedaço pode ser menor que os demais.
func dividirEmLinhas(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	var linhas []string
	for i := 0; i < len(text); i += width {
		fim := i + width
		if fim > len(text) {
			fim = len(text)
		}
		linhas = append(linhas, text[i:fim])
	}
	return linhas
}

// relatarDiferenca aponta onde a linha digitada divergiu da esperada.
//
// Mostrar a posição do erro não enfraquece nada — quem está digitando já tem o
// texto na frente. Serve só para não deixar a pessoa caçando um caractere
// errado no meio de cinquenta.
func relatarDiferenca(out *os.File, esperado, digitado string) {
	fmt.Fprintln(out, "Linha incorreta.")

	minLen := len(digitado)
	if len(esperado) < minLen {
		minLen = len(esperado)
	}

	for i := 0; i < minLen; i++ {
		if digitado[i] != esperado[i] {
			fmt.Fprintf(out, "Erro na posição %d: esperado '%c', digitado '%c'\n",
				i+1, esperado[i], digitado[i])
			return
		}
	}

	if len(digitado) < len(esperado) {
		fmt.Fprintf(out, "Faltaram caracteres: digitou %d, esperado %d.\n", len(digitado), len(esperado))
	} else {
		fmt.Fprintf(out, "Sobraram caracteres: digitou %d, esperado %d.\n", len(digitado), len(esperado))
	}
}
