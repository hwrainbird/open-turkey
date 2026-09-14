// Comandos de agendamento: "open-turkey block schedule ...".
//
// A sintaxe foi escolhida para ser lida em voz alta sem tradução:
//
//	block schedule foco mon-fri 17:30-24:00
//	block schedule-except ia sat 10:00-10:05
//	block schedule-clear foco
//
// "schedule" acrescenta uma janela em que o bloco fica ativo.
// "schedule-except" faz o contrário: o bloco fica ativo o tempo todo, menos
// naquela janela. É o caso "IA bloqueada sempre, liberada cinco minutos no
// sábado" — expressá-lo como janelas soltas daria oito linhas de comando, e
// oito chances de errar uma delas.
package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/brunodcdo/open-turkey/internal/lock"
	"github.com/spf13/cobra"
)

// diasPorNome mapeia os nomes aceitos na linha de comando para os dias do Go.
// Aceitamos inglês e português porque o programa é lido nas duas línguas: os
// comentários e mensagens são em português, mas o README e quem usa o CLI
// costumam estar em inglês.
var diasPorNome = map[string]time.Weekday{
	"sun": time.Sunday, "dom": time.Sunday,
	"mon": time.Monday, "seg": time.Monday,
	"tue": time.Tuesday, "ter": time.Tuesday,
	"wed": time.Wednesday, "qua": time.Wednesday,
	"thu": time.Thursday, "qui": time.Thursday,
	"fri": time.Friday, "sex": time.Friday,
	"sat": time.Saturday, "sab": time.Saturday, "sáb": time.Saturday,
}

// ordemDosDias existe porque um intervalo como "mon-fri" precisa de uma noção
// de "seguinte". Começa no domingo para bater com a numeração do pacote time.
var ordemDosDias = []time.Weekday{
	time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
	time.Thursday, time.Friday, time.Saturday,
}

// ParseDias interpreta a parte de dias da linha de comando.
//
// Formas aceitas:
//
//	mon              → segunda
//	mon,wed,fri      → lista
//	mon-fri          → intervalo (pode dar a volta na semana: fri-mon)
//	weekdays         → segunda a sexta
//	weekend          → sábado e domingo
//	daily / all      → todos os dias
//
// Devolve os dias sem repetição e em ordem, para que a mesma entrada escrita
// de jeitos diferentes produza sempre a mesma agenda.
func ParseDias(entrada string) ([]time.Weekday, error) {
	texto := strings.ToLower(strings.TrimSpace(entrada))
	if texto == "" {
		return nil, fmt.Errorf("nenhum dia informado")
	}

	conjunto := map[time.Weekday]bool{}

	for _, parte := range strings.Split(texto, ",") {
		parte = strings.TrimSpace(parte)
		switch parte {
		case "daily", "all", "todos":
			for _, d := range ordemDosDias {
				conjunto[d] = true
			}
			continue
		case "weekdays", "uteis", "úteis":
			for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
				conjunto[d] = true
			}
			continue
		case "weekend", "fds":
			conjunto[time.Saturday] = true
			conjunto[time.Sunday] = true
			continue
		}

		if inicio, fim, achou := strings.Cut(parte, "-"); achou {
			di, ok := diasPorNome[strings.TrimSpace(inicio)]
			if !ok {
				return nil, fmt.Errorf("dia desconhecido: %q", inicio)
			}
			df, ok := diasPorNome[strings.TrimSpace(fim)]
			if !ok {
				return nil, fmt.Errorf("dia desconhecido: %q", fim)
			}
			// Caminhamos de di até df dando a volta na semana se preciso,
			// para que "fri-mon" signifique sex, sáb, dom, seg.
			for d := di; ; d = (d + 1) % 7 {
				conjunto[d] = true
				if d == df {
					break
				}
			}
			continue
		}

		d, ok := diasPorNome[parte]
		if !ok {
			return nil, fmt.Errorf("dia desconhecido: %q (use mon, tue, ... ou weekdays/weekend/daily)", parte)
		}
		conjunto[d] = true
	}

	dias := make([]time.Weekday, 0, len(conjunto))
	for d := range conjunto {
		dias = append(dias, d)
	}
	sort.Slice(dias, func(i, j int) bool { return dias[i] < dias[j] })
	return dias, nil
}

// ParseJanela interpreta "17:30-24:00" e devolve os minutos desde a meia-noite.
//
// Aceitamos "24:00" como fim porque "até o fim do dia" é o caso mais comum e
// escrever "23:59" deixaria um minuto destravado — um minuto que, com um bloco
// de verdade em jogo, alguém acabaria encontrando.
func ParseJanela(entrada string) (int, int, error) {
	texto := strings.TrimSpace(entrada)
	inicio, fim, achou := strings.Cut(texto, "-")
	if !achou {
		return 0, 0, fmt.Errorf("janela inválida: %q (use hh:mm-hh:mm, ex. 17:30-24:00)", entrada)
	}

	minInicio, err := parseHora(inicio)
	if err != nil {
		return 0, 0, err
	}
	minFim, err := parseHora(fim)
	if err != nil {
		return 0, 0, err
	}
	if minFim <= minInicio {
		return 0, 0, fmt.Errorf("a janela %q termina antes de começar; para atravessar a meia-noite, use duas janelas", entrada)
	}
	return minInicio, minFim, nil
}

func parseHora(s string) (int, error) {
	s = strings.TrimSpace(s)
	h, m, achou := strings.Cut(s, ":")
	if !achou {
		return 0, fmt.Errorf("horário inválido: %q (use hh:mm)", s)
	}
	hora, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil {
		return 0, fmt.Errorf("hora inválida em %q", s)
	}
	minuto, err := strconv.Atoi(strings.TrimSpace(m))
	if err != nil {
		return 0, fmt.Errorf("minuto inválido em %q", s)
	}
	if hora < 0 || hora > 24 || minuto < 0 || minuto > 59 || (hora == 24 && minuto != 0) {
		return 0, fmt.Errorf("horário fora do dia: %q", s)
	}
	return hora*60 + minuto, nil
}

// Complemento devolve as janelas que cobrem o dia inteiro menos a janela dada.
//
// É isso que transforma "liberado das 10:00 às 10:05" em "bloqueado das 00:00
// às 10:00 e das 10:05 às 24:00".
func Complemento(inicio, fim int) [][2]int {
	var janelas [][2]int
	if inicio > 0 {
		janelas = append(janelas, [2]int{0, inicio})
	}
	if fim < db.MinutosNoDia {
		janelas = append(janelas, [2]int{fim, db.MinutosNoDia})
	}
	return janelas
}

// --------------------------------------------------------------------------
// Comandos
// --------------------------------------------------------------------------

var blockScheduleCmd = &cobra.Command{
	Use:   "schedule [bloco] [dias] [hh:mm-hh:mm]",
	Short: "Agendar janelas em que o bloco fica ativo",
	Long: `Agenda janelas semanais em que o bloco fica ativo automaticamente.

O daemon liga o bloco quando a janela abre e o desliga quando ela fecha.
Blocos ligados na mão com "start" não são tocados pela agenda.

Exemplos:
  open-turkey block schedule foco mon-fri 17:30-24:00
  open-turkey block schedule foco sun 17:30-24:00
  open-turkey block schedule leitura weekend 09:00-12:00

Dias: mon tue wed thu fri sat sun, ou weekdays, weekend, daily.
Intervalos (mon-fri) e listas (mon,wed,fri) são aceitos.`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return agendar(cmd, args[0], args[1], args[2], false)
	},
}

var blockScheduleExceptCmd = &cobra.Command{
	Use:   "schedule-except [bloco] [dias] [hh:mm-hh:mm]",
	Short: "Manter o bloco ativo o tempo todo, exceto na janela informada",
	Long: `Deixa o bloco ativo 24 horas por dia, todos os dias, com uma única
janela de folga nos dias indicados.

É o caso "bloqueado sempre, liberado cinco minutos no sábado". Escrito como
janelas soltas isso daria oito comandos; aqui é um só.

Exemplo:
  open-turkey block schedule-except ia sat 10:00-10:05

Os outros seis dias ficam cobertos de 00:00 a 24:00, e o sábado fica coberto
de 00:00 a 10:00 e de 10:05 a 24:00.`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return agendar(cmd, args[0], args[1], args[2], true)
	},
}

var blockScheduleClearCmd = &cobra.Command{
	Use:   "schedule-clear [bloco]",
	Short: "Remover todas as janelas agendadas de um bloco",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nome := args[0]

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		// Limpar a agenda de um bloco travado e ativo equivaleria a desativá-lo
		// sem desafio: bastaria esperar a janela fechar. Cobramos o mesmo preço
		// de qualquer outra forma de afrouxar um bloco travado.
		if err := autorizarRemocao(database, nome); err != nil {
			return err
		}

		if err := database.ClearSchedules(nome); err != nil {
			return err
		}

		fmt.Printf("Agenda do bloco '%s' removida.\n", nome)
		return nil
	},
}

// agendar é o corpo compartilhado por "schedule" e "schedule-except".
func agendar(cmd *cobra.Command, nome, diasTexto, janelaTexto string, inverter bool) error {
	dias, err := ParseDias(diasTexto)
	if err != nil {
		return err
	}
	inicio, fim, err := ParseJanela(janelaTexto)
	if err != nil {
		return err
	}

	semTrava, err := cmd.Flags().GetBool("no-lock")
	if err != nil {
		return err
	}
	lockChars, err := cmd.Flags().GetInt("lock-chars")
	if err != nil {
		return err
	}
	if !semTrava && (lockChars < lock.MinLockChars || lockChars > lock.MaxLockChars) {
		return fmt.Errorf("--lock-chars precisa estar entre %d e %d (recebido: %d)",
			lock.MinLockChars, lock.MaxLockChars, lockChars)
	}
	travar := !semTrava

	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if _, err := database.GetBlock(nome); err != nil {
		return err
	}

	// Mexer na agenda de um bloco travado e ativo muda quando ele vai cair.
	// Vale o mesmo preço de remover sites dele.
	if err := autorizarRemocao(database, nome); err != nil {
		return err
	}

	type janela struct {
		dia         time.Weekday
		inicio, fim int
	}
	var aGravar []janela

	if inverter {
		// Dias citados: o dia inteiro menos a folga.
		folga := map[time.Weekday]bool{}
		for _, d := range dias {
			folga[d] = true
			for _, j := range Complemento(inicio, fim) {
				aGravar = append(aGravar, janela{d, j[0], j[1]})
			}
		}
		// Todos os outros dias: cobertos por inteiro.
		for _, d := range ordemDosDias {
			if !folga[d] {
				aGravar = append(aGravar, janela{d, 0, db.MinutosNoDia})
			}
		}
	} else {
		for _, d := range dias {
			aGravar = append(aGravar, janela{d, inicio, fim})
		}
	}

	for _, j := range aGravar {
		if err := database.AddSchedule(nome, j.dia, j.inicio, j.fim, travar, lockChars); err != nil {
			return err
		}
	}

	janelas, err := database.GetSchedulesByName(nome)
	if err != nil {
		return err
	}

	fmt.Printf("Agenda do bloco '%s' atualizada (%d janelas no total):\n", nome, len(janelas))
	for _, j := range janelas {
		trava := "sem trava"
		if j.Locked {
			trava = fmt.Sprintf("trava de %d caracteres", j.LockChars)
		}
		fmt.Printf("  %s  (%s)\n", j.Descreve(), trava)
	}
	if travar {
		fmt.Println("\nO daemon liga e desliga este bloco sozinho. Para desligá-lo antes da hora,")
		fmt.Println("use 'open-turkey unlock' — a agenda fica suspensa só até o fim da janela atual.")
	}
	return nil
}

func init() {
	for _, c := range []*cobra.Command{blockScheduleCmd, blockScheduleExceptCmd} {
		c.Flags().Bool("no-lock", false, "Agendar sem trava (permite desativar com 'stop')")
		c.Flags().Int("lock-chars", lock.DefaultLockChars, "Caracteres do desafio para desativar antes da hora")
	}

	blockCmd.AddCommand(blockScheduleCmd)
	blockCmd.AddCommand(blockScheduleExceptCmd)
	blockCmd.AddCommand(blockScheduleClearCmd)
}
