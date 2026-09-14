// Folgas: uma janela curta que VOCÊ escolhe quando abrir.
//
// === O PROBLEMA COM A JANELA FIXA ===
//
// Uma agenda do tipo "liberado sábado das 10:00 às 10:05" tem um defeito
// prático: obriga a pessoa a estar na frente do computador às dez em ponto.
// Perdeu a hora, perdeu a semana. O compromisso era razoável, mas a forma de
// cobrá-lo não.
//
// A folga separa as duas coisas. A regra continua sendo um compromisso feito
// com antecedência — quantos minutos, em que dias, quantas vezes —, mas o
// momento de usá-la é decidido na hora.
//
// === POR QUE NÃO COBRA O DESAFIO ===
//
// Porque a folga NÃO é uma forma de burlar o bloqueio: ela é o bloqueio
// funcionando como combinado. O desafio de digitação existe para quebrar a
// regra; a folga é a regra. Cobrar o desafio aqui puniria o uso correto da
// ferramenta e empurraria a pessoa para o caminho de fora dela.
//
// O que impede o abuso é o limite: N minutos, nos dias definidos, no máximo M
// vezes por dia. Acabou a cota, o único caminho é o desafio.
//
// === COMO É IMPLEMENTADA ===
//
// Reaproveitando a supressão que já existe para o "unlock". Uma folga é uma
// supressão que você pede em vez de merecer: o bloco é desativado e a agenda
// fica proibida de religá-lo até o fim da folga. Passado o prazo, o ciclo
// seguinte do daemon reativa tudo sozinho, sem nenhuma intervenção.
package db

import (
	"fmt"
	"time"
)

// RecessPolicy é a regra: em que dia, dentro de que janela, por quantos
// minutos e quantas vezes por dia uma folga pode ser pedida.
//
// StartMin/EndMin delimitam quando a folga PODE SER PEDIDA, e não quanto ela
// dura. Uma política de sábado 09:00–12:00 com 30 minutos significa "peça
// entre nove e meio-dia, e leve trinta minutos a partir do momento em que
// pedir" — inclusive se isso passar do meio-dia.
type RecessPolicy struct {
	ID        int
	Weekday   time.Weekday
	StartMin  int
	EndMin    int
	Minutes   int
	MaxPerDay int
}

// Descreve devolve a política em texto legível.
func (r RecessPolicy) Descreve() string {
	vezes := "1 vez"
	if r.MaxPerDay != 1 {
		vezes = fmt.Sprintf("%d vezes", r.MaxPerDay)
	}
	janela := ""
	if r.StartMin != 0 || r.EndMin != MinutosNoDia {
		janela = fmt.Sprintf(" entre %s e %s", hhmm(r.StartMin), hhmm(r.EndMin))
	}
	return fmt.Sprintf("%s: %d min, %s por dia%s", nomeDoDia(r.Weekday), r.Minutes, vezes, janela)
}

// AddRecessPolicy grava uma regra de folga.
func (d *DB) AddRecessPolicy(blockName string, weekday time.Weekday, startMin, endMin, minutes, maxPerDay int) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	if weekday < time.Sunday || weekday > time.Saturday {
		return fmt.Errorf("dia da semana inválido: %d", weekday)
	}
	if minutes <= 0 {
		return fmt.Errorf("a folga precisa ter pelo menos 1 minuto (recebido: %d)", minutes)
	}
	if minutes > MinutosNoDia {
		return fmt.Errorf("uma folga não pode passar de um dia inteiro (recebido: %d minutos)", minutes)
	}
	if maxPerDay <= 0 {
		return fmt.Errorf("o número de folgas por dia precisa ser pelo menos 1 (recebido: %d)", maxPerDay)
	}
	if startMin < 0 || endMin > MinutosNoDia || endMin <= startMin {
		return fmt.Errorf("janela inválida para pedir a folga: %s–%s", hhmm(startMin), hhmm(endMin))
	}

	_, err = d.conn.Exec(
		`INSERT INTO recess_policies (block_id, weekday, start_min, end_min, minutes, max_per_day)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		blockID, int(weekday), startMin, endMin, minutes, maxPerDay,
	)
	if err != nil {
		return fmt.Errorf("erro ao gravar a regra de folga de '%s': %w", blockName, err)
	}
	return nil
}

// GetRecessPolicies devolve as regras de folga de um bloco.
func (d *DB) GetRecessPolicies(blockID int) ([]RecessPolicy, error) {
	rows, err := d.conn.Query(
		`SELECT id, weekday, start_min, end_min, minutes, max_per_day
		 FROM recess_policies WHERE block_id = ? ORDER BY weekday, start_min`, blockID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar regras de folga: %w", err)
	}
	defer rows.Close()

	var politicas []RecessPolicy
	for rows.Next() {
		var r RecessPolicy
		var dia int
		if err := rows.Scan(&r.ID, &dia, &r.StartMin, &r.EndMin, &r.Minutes, &r.MaxPerDay); err != nil {
			return nil, fmt.Errorf("erro ao ler regra de folga: %w", err)
		}
		r.Weekday = time.Weekday(dia)
		politicas = append(politicas, r)
	}
	return politicas, rows.Err()
}

// GetRecessPoliciesByName devolve as regras de folga pelo nome do bloco.
func (d *DB) GetRecessPoliciesByName(blockName string) ([]RecessPolicy, error) {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return nil, err
	}
	return d.GetRecessPolicies(blockID)
}

// ClearRecessPolicies apaga todas as regras de folga de um bloco.
func (d *DB) ClearRecessPolicies(blockName string) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}
	if _, err := d.conn.Exec("DELETE FROM recess_policies WHERE block_id = ?", blockID); err != nil {
		return fmt.Errorf("erro ao limpar as regras de folga de '%s': %w", blockName, err)
	}
	return nil
}

// PoliticaAplicavel devolve a regra que vale neste instante.
//
// Quando mais de uma casa, vale a mais generosa em minutos: se a pessoa se
// deu duas regras sobrepostas, o razoável é honrar a melhor, não a primeira
// que a consulta devolveu.
func PoliticaAplicavel(now time.Time, politicas []RecessPolicy) (RecessPolicy, bool) {
	minuto := MinutoDoDia(now)
	dia := now.Weekday()

	var melhor RecessPolicy
	achou := false

	for _, p := range politicas {
		if p.Weekday != dia || minuto < p.StartMin || minuto >= p.EndMin {
			continue
		}
		if !achou || p.Minutes > melhor.Minutes {
			melhor = p
			achou = true
		}
	}
	return melhor, achou
}

// FolgasUsadasHoje conta quantas folgas o bloco já teve no dia de hoje.
//
// A contagem é pelo DIA LOCAL, não pelas últimas 24 horas. "Uma vez por dia"
// é uma frase sobre o calendário: uma folga às 23h de sábado e outra à 1h de
// domingo são dois dias diferentes, ainda que separadas por duas horas.
func (d *DB) FolgasUsadasHoje(blockID int, now time.Time) (int, error) {
	// Buscamos uma janela folgada e filtramos em Go. Comparar datas locais em
	// SQL exigiria confiar no fuso do SQLite, que não é o mesmo do processo.
	desde := now.Add(-48 * time.Hour).UTC()

	rows, err := d.conn.Query(
		"SELECT started_at FROM recess_log WHERE block_id = ? AND started_at >= ?", blockID, desde)
	if err != nil {
		return 0, fmt.Errorf("erro ao contar folgas: %w", err)
	}
	defer rows.Close()

	anoHoje, mesHoje, diaHoje := now.Date()
	usadas := 0

	for rows.Next() {
		var quando time.Time
		if err := rows.Scan(&quando); err != nil {
			return 0, fmt.Errorf("erro ao ler registro de folga: %w", err)
		}
		a, m, dd := quando.In(now.Location()).Date()
		if a == anoHoje && m == mesHoje && dd == diaHoje {
			usadas++
		}
	}
	return usadas, rows.Err()
}

// RegistrarFolga grava que uma folga foi usada.
func (d *DB) RegistrarFolga(blockID int, now time.Time, minutes int) error {
	_, err := d.conn.Exec(
		"INSERT INTO recess_log (block_id, started_at, minutes) VALUES (?, ?, ?)",
		blockID, now.UTC(), minutes)
	if err != nil {
		return fmt.Errorf("erro ao registrar a folga: %w", err)
	}
	return nil
}

// ProximaFolga diz quando a próxima folga poderá ser pedida.
//
// Serve para a mensagem de recusa. "Não pode agora" sozinho obriga a pessoa a
// ir ler a configuração; dizer "sábado a partir das 09:00" responde a pergunta
// que ela realmente tem.
func ProximaFolga(now time.Time, politicas []RecessPolicy) (time.Time, bool) {
	if len(politicas) == 0 {
		return time.Time{}, false
	}

	meiaNoite := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Olhamos os próximos oito dias: sete cobrem a semana toda, e o oitavo
	// cobre o caso de a janela de hoje já ter passado.
	for avanco := 0; avanco <= 8; avanco++ {
		dia := meiaNoite.AddDate(0, 0, avanco)
		for _, p := range politicas {
			if p.Weekday != dia.Weekday() {
				continue
			}
			inicio := dia.Add(time.Duration(p.StartMin) * time.Minute)
			if inicio.After(now) {
				return inicio, true
			}
		}
	}
	return time.Time{}, false
}
