// Comandos de folga.
//
//	block recess-policy ai sat --minutes 30
//	block recess-policy ai sat --minutes 30 --window 09:00-12:00 --times 2
//	block recess-clear ai
//	recess ai
//
// A regra é um compromisso feito com antecedência; "recess" é o momento de
// usá-la. Ver internal/db/recess.go para o raciocínio por trás da separação.
package cli

import (
	"fmt"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/spf13/cobra"
)

var blockRecessPolicyCmd = &cobra.Command{
	Use:   "recess-policy [bloco] [dias]",
	Short: "Permitir folgas curtas, pedidas na hora, em certos dias",
	Long: `Define quando este bloco pode ter uma folga e de quanto tempo.

Ao contrário de uma janela agendada, a folga não tem hora marcada: você a pede
quando quiser, dentro dos dias permitidos, e ela dura os minutos combinados a
partir daquele instante.

Pedir a folga NÃO exige o desafio de digitação — ela é o bloqueio funcionando
como combinado, não uma forma de furá-lo. O que limita o uso é a cota.

Exemplos:
  open-turkey block recess-policy ai sat --minutes 30
  open-turkey block recess-policy ai sat --minutes 30 --window 09:00-12:00
  open-turkey block recess-policy ai weekend --minutes 15 --times 2

--window limita quando a folga pode ser PEDIDA, não quanto ela dura: uma folga
de 30 minutos pedida às 11:55 vale até 12:25.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		nome, diasTexto := args[0], args[1]

		dias, err := ParseDias(diasTexto)
		if err != nil {
			return err
		}

		minutos, err := cmd.Flags().GetInt("minutes")
		if err != nil {
			return err
		}
		vezes, err := cmd.Flags().GetInt("times")
		if err != nil {
			return err
		}
		janelaTexto, err := cmd.Flags().GetString("window")
		if err != nil {
			return err
		}

		inicio, fim := 0, db.MinutosNoDia
		if janelaTexto != "" {
			inicio, fim, err = ParseJanela(janelaTexto)
			if err != nil {
				return err
			}
		}

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		if _, err := database.GetBlock(nome); err != nil {
			return err
		}

		// Criar ou afrouxar uma regra de folga muda quanto tempo livre o bloco
		// concede. Num bloco travado e ativo, isso custa o mesmo que remover
		// sites dele.
		if err := autorizarRemocao(database, nome); err != nil {
			return err
		}

		for _, d := range dias {
			if err := database.AddRecessPolicy(nome, d, inicio, fim, minutos, vezes); err != nil {
				return err
			}
		}

		politicas, err := database.GetRecessPoliciesByName(nome)
		if err != nil {
			return err
		}

		fmt.Printf("Regras de folga do bloco '%s':\n", nome)
		for _, p := range politicas {
			fmt.Printf("  %s\n", p.Descreve())
		}
		fmt.Printf("\nPara usar: open-turkey recess %s\n", nome)
		return nil
	},
}

var blockRecessClearCmd = &cobra.Command{
	Use:   "recess-clear [bloco]",
	Short: "Remover todas as regras de folga de um bloco",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nome := args[0]

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		// Apertar o cerco é livre — tirar as folgas deixa o bloco MAIS
		// restrito, então não há o que cobrar.
		if err := database.ClearRecessPolicies(nome); err != nil {
			return err
		}

		fmt.Printf("Regras de folga do bloco '%s' removidas.\n", nome)
		return nil
	},
}

var recessCmd = &cobra.Command{
	Use:   "recess [bloco]",
	Short: "Usar uma folga curta agora, dentro da cota",
	Long: `Abre agora a folga combinada para este bloco.

O bloco é desativado pelos minutos definidos na regra e volta sozinho quando o
tempo acabar — não é preciso fazer nada para religá-lo.

Sem desafio de digitação: a cota é o que limita. Esgotada a cota do dia, o
único caminho é 'open-turkey unlock'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nome := args[0]
		agora := time.Now()

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		detalhe, err := database.GetBlock(nome)
		if err != nil {
			return err
		}

		politicas, err := database.GetRecessPolicies(detalhe.ID)
		if err != nil {
			return err
		}
		if len(politicas) == 0 {
			return fmt.Errorf("o bloco '%s' não tem folgas configuradas. Defina uma com 'open-turkey block recess-policy'", nome)
		}

		politica, permitido := db.PoliticaAplicavel(agora, politicas)
		if !permitido {
			// Dizer só "não pode agora" obrigaria a pessoa a ir ler a
			// configuração. Dizemos quando poderá.
			if proxima, ok := db.ProximaFolga(agora, politicas); ok {
				return fmt.Errorf("sem folga disponível agora. A próxima é %s",
					proxima.Format("segunda-feira, 02/01 às 15:04"))
			}
			return fmt.Errorf("sem folga disponível agora para o bloco '%s'", nome)
		}

		usadas, err := database.FolgasUsadasHoje(detalhe.ID, agora)
		if err != nil {
			return err
		}
		if usadas >= politica.MaxPerDay {
			return fmt.Errorf("a cota de folgas de hoje já foi usada (%d de %d). Para desativar mesmo assim: open-turkey unlock %s",
				usadas, politica.MaxPerDay, nome)
		}

		fim := agora.Add(time.Duration(politica.Minutes) * time.Minute)

		if err := database.RegistrarFolga(detalhe.ID, agora, politica.Minutes); err != nil {
			return err
		}

		// A ordem importa: suprimimos ANTES de desativar. Se o daemon rodasse
		// entre as duas operações, veria a janela aberta e o bloco parado, e
		// religaria tudo antes de a folga começar.
		if err := database.SetSuppression(nome, fim); err != nil {
			return err
		}

		if detalhe.Active {
			if err := database.DeactivateBlock(nome); err != nil {
				return err
			}
		}

		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}

		restantes := politica.MaxPerDay - usadas - 1
		fmt.Printf("Folga de %d minutos aberta no bloco '%s'.\n", politica.Minutes, nome)
		fmt.Printf("Volta a bloquear às %s, sozinho.\n", fim.Format("15:04"))
		if restantes > 0 {
			fmt.Printf("Ainda restam %d folgas hoje.\n", restantes)
		} else {
			fmt.Println("Era a última folga de hoje.")
		}
		return nil
	},
}

func init() {
	blockRecessPolicyCmd.Flags().Int("minutes", 30, "Duração da folga, em minutos")
	blockRecessPolicyCmd.Flags().Int("times", 1, "Quantas folgas por dia")
	blockRecessPolicyCmd.Flags().String("window", "", "Limita quando a folga pode ser pedida (ex. 09:00-12:00)")

	blockCmd.AddCommand(blockRecessPolicyCmd)
	blockCmd.AddCommand(blockRecessClearCmd)
	rootCmd.AddCommand(recessCmd)
}
