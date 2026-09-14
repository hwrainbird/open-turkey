// Agendamento de blocos.
//
// === O PROBLEMA ===
//
// Até aqui, um bloco só ficava ativo se alguém digitasse "open-turkey start".
// Isso serve para "vou focar agora", mas não para os dois casos que mais
// importam na prática:
//
//   - "bloqueie isso sempre, exceto numa janela combinada" (ex.: IA liberada
//     só aos sábados, das 10h às 10h05)
//   - "bloqueie isso todo dia a partir das 17h30"
//
// Ambos são compromissos feitos com antecedência, quando a cabeça está fria.
// É justamente o contrário de uma decisão por impulso — por isso faz sentido
// que o daemon os aplique sozinho.
//
// === COMO FUNCIONA ===
//
// Uma agenda é um conjunto de janelas semanais. Cada janela diz: neste dia da
// semana, entre estes dois horários, este bloco deve estar ativo.
//
// O daemon compara o relógio com as janelas a cada 5 segundos e faz o óbvio:
// abriu a janela, ativa o bloco; fechou, desativa. Ativar por agenda é igual a
// ativar na mão — a linha vai para a mesma tabela active_blocks —, então
// "status", "stop" e "unlock" continuam funcionando sem saber que agendas
// existem.
//
// === POR QUE A TABELA DE SUPRESSÃO ===
//
// Se você destravar um bloco agendado no meio de uma janela, o daemon veria a
// janela ainda aberta e reativaria o bloco cinco segundos depois. O desafio de
// digitação não teria servido para nada.
//
// A supressão resolve isso: destravar dentro de uma janela marca esse bloco
// como "não reative até a janela atual terminar". A próxima janela — no dia
// seguinte, tipicamente — volta a valer normalmente. É exatamente o
// comportamento que se espera: você comprou o resto de hoje, não o resto da
// semana.
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// MinutosNoDia é quantos minutos um dia tem. Usado como limite superior de uma
// janela que vai até a meia-noite.
const MinutosNoDia = 24 * 60

// Schedule é uma janela semanal.
//
// Weekday segue a convenção do pacote time do Go: domingo é 0, sábado é 6.
// Usamos a mesma numeração para não precisar converter nada na hora de
// comparar com o relógio.
//
// StartMin e EndMin são minutos desde a meia-noite. 17h30 vira 1050.
// EndMin é exclusivo: uma janela 0–1440 cobre o dia inteiro, e uma janela que
// termina às 18h00 (1080) não inclui o minuto das 18h00.
//
// Janelas não atravessam a meia-noite. Uma janela do tipo "22h às 2h" é
// gravada como duas: 22h–24h num dia e 0h–2h no dia seguinte. Quem faz essa
// divisão é o CLI, para que a comparação aqui continue trivial.
type Schedule struct {
	ID        int
	Weekday   time.Weekday
	StartMin  int
	EndMin    int
	Locked    bool
	LockChars int
}

// Descreve devolve a janela em texto legível, ex.: "seg 17:30–24:00".
func (s Schedule) Descreve() string {
	return fmt.Sprintf("%s %s–%s", nomeDoDia(s.Weekday), hhmm(s.StartMin), hhmm(s.EndMin))
}

func hhmm(min int) string {
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

var nomesDosDias = [7]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

func nomeDoDia(d time.Weekday) string {
	if d < 0 || int(d) >= len(nomesDosDias) {
		return "?"
	}
	return nomesDosDias[d]
}

// MinutoDoDia converte um horário em minutos desde a meia-noite.
func MinutoDoDia(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// DentroDaJanela diz se o instante informado cai em alguma das janelas.
//
// É uma função pura — sem banco, sem relógio interno — justamente para poder
// ser testada com horários fixos em vez de "agora".
func DentroDaJanela(now time.Time, janelas []Schedule) bool {
	_, dentro := JanelaAtual(now, janelas)
	return dentro
}

// JanelaAtual devolve a janela que contém o instante informado.
//
// O segundo retorno diz se alguma janela casou. Quando mais de uma casa (o que
// é permitido: janelas podem se sobrepor), devolvemos a que termina mais tarde,
// porque é ela que determina até quando o bloco fica de pé.
func JanelaAtual(now time.Time, janelas []Schedule) (Schedule, bool) {
	minuto := MinutoDoDia(now)
	dia := now.Weekday()

	var melhor Schedule
	achou := false

	for _, j := range janelas {
		if j.Weekday != dia {
			continue
		}
		if minuto < j.StartMin || minuto >= j.EndMin {
			continue
		}
		if !achou || j.EndMin > melhor.EndMin {
			melhor = j
			achou = true
		}
	}

	return melhor, achou
}

// FimDaJanela devolve o instante em que a janela atual se fecha.
//
// Usado ao destravar um bloco agendado: suprimimos a reativação até esse
// instante. Se nenhuma janela estiver aberta, devolvemos o próprio "now" —
// não há nada a suprimir.
func FimDaJanela(now time.Time, janelas []Schedule) time.Time {
	janela, dentro := JanelaAtual(now, janelas)
	if !dentro {
		return now
	}

	meiaNoite := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return meiaNoite.Add(time.Duration(janela.EndMin) * time.Minute)
}

// --------------------------------------------------------------------------
// Leitura e escrita de agendas
// --------------------------------------------------------------------------

// AddSchedule acrescenta uma janela a um bloco.
//
// Valida os limites aqui, e não só no CLI, porque o banco é a fonte da verdade:
// uma janela inválida gravada por engano faria o daemon tomar decisões erradas
// para sempre, sem nenhum erro visível.
func (d *DB) AddSchedule(blockName string, weekday time.Weekday, startMin, endMin int, locked bool, lockChars int) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	if weekday < time.Sunday || weekday > time.Saturday {
		return fmt.Errorf("dia da semana inválido: %d (use 0=domingo a 6=sábado)", weekday)
	}
	if startMin < 0 || startMin >= MinutosNoDia {
		return fmt.Errorf("horário de início fora do dia: %d minutos", startMin)
	}
	if endMin <= startMin || endMin > MinutosNoDia {
		return fmt.Errorf("janela inválida: %s–%s (o fim precisa vir depois do início e não passar da meia-noite)",
			hhmm(startMin), hhmm(endMin))
	}

	_, err = d.conn.Exec(
		"INSERT INTO schedules (block_id, weekday, start_min, end_min, locked, lock_chars) VALUES (?, ?, ?, ?, ?, ?)",
		blockID, int(weekday), startMin, endMin, locked, lockChars,
	)
	if err != nil {
		return fmt.Errorf("erro ao agendar bloco '%s': %w", blockName, err)
	}
	return nil
}

// GetSchedules devolve as janelas de um bloco, pelo ID.
func (d *DB) GetSchedules(blockID int) ([]Schedule, error) {
	rows, err := d.conn.Query(
		"SELECT id, weekday, start_min, end_min, locked, lock_chars FROM schedules WHERE block_id = ? ORDER BY weekday, start_min",
		blockID,
	)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar agendas do bloco: %w", err)
	}
	defer rows.Close()

	var janelas []Schedule
	for rows.Next() {
		var s Schedule
		var dia int
		if err := rows.Scan(&s.ID, &dia, &s.StartMin, &s.EndMin, &s.Locked, &s.LockChars); err != nil {
			return nil, fmt.Errorf("erro ao ler agenda: %w", err)
		}
		s.Weekday = time.Weekday(dia)
		janelas = append(janelas, s)
	}
	return janelas, rows.Err()
}

// GetSchedulesByName devolve as janelas de um bloco, pelo nome.
func (d *DB) GetSchedulesByName(blockName string) ([]Schedule, error) {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return nil, err
	}
	return d.GetSchedules(blockID)
}

// ClearSchedules apaga todas as janelas de um bloco.
func (d *DB) ClearSchedules(blockName string) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}
	if _, err := d.conn.Exec("DELETE FROM schedules WHERE block_id = ?", blockID); err != nil {
		return fmt.Errorf("erro ao limpar agendas do bloco '%s': %w", blockName, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Supressão
// --------------------------------------------------------------------------

// SetSuppression impede que a agenda reative o bloco até o instante informado.
func (d *DB) SetSuppression(blockName string, until time.Time) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(
		"INSERT INTO suppressions (block_id, until) VALUES (?, ?) ON CONFLICT(block_id) DO UPDATE SET until = excluded.until",
		blockID, until.UTC(),
	)
	if err != nil {
		return fmt.Errorf("erro ao suprimir a agenda do bloco '%s': %w", blockName, err)
	}
	return nil
}

// GetSuppression devolve até quando a agenda do bloco está suprimida.
// O segundo retorno é false quando não há supressão registrada.
func (d *DB) GetSuppression(blockID int) (time.Time, bool, error) {
	var until time.Time
	err := d.conn.QueryRow("SELECT until FROM suppressions WHERE block_id = ?", blockID).Scan(&until)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("erro ao ler supressão: %w", err)
	}
	return until, true, nil
}

// --------------------------------------------------------------------------
// Reconciliação (o que o daemon chama a cada ciclo)
// --------------------------------------------------------------------------

// MudancaAgendada descreve uma ativação ou desativação feita pela agenda.
type MudancaAgendada struct {
	Bloco  string
	Ativou bool
	Janela string
}

// ReconcileSchedules alinha o estado dos blocos agendados com o relógio.
//
// Para cada bloco que tem agenda:
//
//   - janela aberta e bloco parado  → ativa (com a trava definida na janela)
//   - janela fechada e bloco ativo por agenda → desativa
//   - bloco ativado na mão → não encosta. Quem ligou na mão desliga na mão;
//     a agenda não tem o direito de desfazer uma decisão explícita.
//
// A supressão é consultada apenas na hora de ativar. Uma supressão vencida é
// apagada no caminho, para a tabela não crescer sem necessidade.
func (d *DB) ReconcileSchedules(now time.Time) ([]MudancaAgendada, error) {
	rows, err := d.conn.Query(`
		SELECT DISTINCT b.id, b.name,
		       CASE WHEN ab.id IS NOT NULL THEN 1 ELSE 0 END AS ativo,
		       COALESCE(ab.by_schedule, 0) AS por_agenda
		FROM blocks b
		JOIN schedules s ON s.block_id = b.id
		LEFT JOIN active_blocks ab ON ab.block_id = b.id
	`)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar blocos agendados: %w", err)
	}

	type candidato struct {
		id        int
		nome      string
		ativo     bool
		porAgenda bool
	}

	var candidatos []candidato
	for rows.Next() {
		var c candidato
		if err := rows.Scan(&c.id, &c.nome, &c.ativo, &c.porAgenda); err != nil {
			rows.Close()
			return nil, fmt.Errorf("erro ao ler bloco agendado: %w", err)
		}
		candidatos = append(candidatos, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("erro ao iterar blocos agendados: %w", err)
	}
	// Fechamos antes de escrever: o SQLite não gosta de INSERT/DELETE com um
	// cursor de leitura ainda aberto sobre as mesmas tabelas.
	rows.Close()

	var mudancas []MudancaAgendada

	for _, c := range candidatos {
		janelas, err := d.GetSchedules(c.id)
		if err != nil {
			return nil, err
		}

		janela, dentro := JanelaAtual(now, janelas)

		switch {
		case dentro && !c.ativo:
			suprimidoAte, temSupressao, err := d.GetSuppression(c.id)
			if err != nil {
				return nil, err
			}
			if temSupressao {
				if now.Before(suprimidoAte) {
					continue // destravado há pouco; respeita o resto da janela
				}
				if _, err := d.conn.Exec("DELETE FROM suppressions WHERE block_id = ?", c.id); err != nil {
					return nil, fmt.Errorf("erro ao limpar supressão vencida: %w", err)
				}
			}

			if _, err := d.conn.Exec(
				"INSERT INTO active_blocks (block_id, locked, lock_chars, by_schedule) VALUES (?, ?, ?, 1)",
				c.id, janela.Locked, janela.LockChars,
			); err != nil {
				return nil, fmt.Errorf("erro ao ativar bloco agendado '%s': %w", c.nome, err)
			}
			mudancas = append(mudancas, MudancaAgendada{Bloco: c.nome, Ativou: true, Janela: janela.Descreve()})

		case !dentro && c.ativo && c.porAgenda:
			if _, err := d.conn.Exec("DELETE FROM active_blocks WHERE block_id = ?", c.id); err != nil {
				return nil, fmt.Errorf("erro ao desativar bloco agendado '%s': %w", c.nome, err)
			}
			mudancas = append(mudancas, MudancaAgendada{Bloco: c.nome, Ativou: false})
		}
	}

	return mudancas, nil
}
