package db

import (
	"testing"
	"time"
)

func politicaSabado() RecessPolicy {
	return RecessPolicy{Weekday: time.Saturday, StartMin: 0, EndMin: MinutosNoDia, Minutes: 30, MaxPerDay: 1}
}

func TestPoliticaAplicavelSoNoDiaCerto(t *testing.T) {
	p := []RecessPolicy{politicaSabado()}

	if _, ok := PoliticaAplicavel(sabAs(14, 0), p); !ok {
		t.Error("sábado deveria permitir folga")
	}
	if _, ok := PoliticaAplicavel(segAs(14, 0), p); ok {
		t.Error("segunda não deveria permitir folga")
	}
}

func TestPoliticaAplicavelRespeitaAJanelaDePedido(t *testing.T) {
	p := []RecessPolicy{{Weekday: time.Saturday, StartMin: 9 * 60, EndMin: 12 * 60, Minutes: 30, MaxPerDay: 1}}

	if _, ok := PoliticaAplicavel(sabAs(8, 59), p); ok {
		t.Error("antes da janela não deveria permitir")
	}
	if _, ok := PoliticaAplicavel(sabAs(11, 59), p); !ok {
		t.Error("dentro da janela deveria permitir")
	}
	if _, ok := PoliticaAplicavel(sabAs(12, 0), p); ok {
		t.Error("o fim da janela é exclusivo")
	}
}

// Regras sobrepostas: vale a mais generosa, não a primeira que aparecer.
func TestPoliticaAplicavelPegaAMaisGenerosa(t *testing.T) {
	p := []RecessPolicy{
		{Weekday: time.Saturday, StartMin: 0, EndMin: MinutosNoDia, Minutes: 5, MaxPerDay: 1},
		{Weekday: time.Saturday, StartMin: 0, EndMin: MinutosNoDia, Minutes: 30, MaxPerDay: 1},
	}
	got, ok := PoliticaAplicavel(sabAs(14, 0), p)
	if !ok || got.Minutes != 30 {
		t.Fatalf("queria a regra de 30 min, veio %+v", got)
	}
}

// "Uma vez por dia" é uma frase sobre o calendário, não sobre 24 horas
// corridas: 23h de sábado e 1h de domingo são dias diferentes.
func TestFolgasUsadasHojeContaPeloDiaLocal(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "ai")

	detalhe, err := d.GetBlock("ai")
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}

	sabadoTarde := sabAs(23, 0)
	if err := d.RegistrarFolga(detalhe.ID, sabadoTarde, 30); err != nil {
		t.Fatalf("RegistrarFolga: %v", err)
	}

	usadas, err := d.FolgasUsadasHoje(detalhe.ID, sabadoTarde)
	if err != nil {
		t.Fatalf("FolgasUsadasHoje: %v", err)
	}
	if usadas != 1 {
		t.Errorf("mesmo dia: queria 1, veio %d", usadas)
	}

	domingoCedo := sabadoTarde.Add(2 * time.Hour) // 01:00 de domingo
	usadas, err = d.FolgasUsadasHoje(detalhe.ID, domingoCedo)
	if err != nil {
		t.Fatalf("FolgasUsadasHoje: %v", err)
	}
	if usadas != 0 {
		t.Errorf("dia seguinte: queria 0, veio %d", usadas)
	}
}

func TestProximaFolgaApontaOProximoDiaPermitido(t *testing.T) {
	p := []RecessPolicy{{Weekday: time.Saturday, StartMin: 9 * 60, EndMin: 12 * 60, Minutes: 30, MaxPerDay: 1}}

	// De uma segunda-feira, o próximo sábado às 09:00.
	proxima, ok := ProximaFolga(segAs(14, 0), p)
	if !ok {
		t.Fatal("deveria achar a próxima folga")
	}
	if proxima.Weekday() != time.Saturday || proxima.Hour() != 9 {
		t.Fatalf("queria sábado 09:00, veio %v", proxima)
	}
}

// Se a janela de hoje já passou, a próxima é semana que vem — e não hoje de
// novo, que seria uma data no passado.
func TestProximaFolgaPulaAJanelaJaVencida(t *testing.T) {
	p := []RecessPolicy{{Weekday: time.Saturday, StartMin: 9 * 60, EndMin: 12 * 60, Minutes: 30, MaxPerDay: 1}}

	proxima, ok := ProximaFolga(sabAs(15, 0), p)
	if !ok {
		t.Fatal("deveria achar a próxima folga")
	}
	if !proxima.After(sabAs(15, 0)) {
		t.Fatalf("a próxima folga não pode estar no passado: %v", proxima)
	}
	if proxima.Weekday() != time.Saturday {
		t.Fatalf("queria o sábado seguinte, veio %v", proxima)
	}
}

func TestAddRecessPolicyRecusaValoresInvalidos(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "ai")

	casos := []struct {
		nome                     string
		minutos, vezes, ini, fim int
	}{
		{"zero minutos", 0, 1, 0, MinutosNoDia},
		{"minutos negativos", -5, 1, 0, MinutosNoDia},
		{"mais que um dia", MinutosNoDia + 1, 1, 0, MinutosNoDia},
		{"zero vezes", 30, 0, 0, MinutosNoDia},
		{"janela invertida", 30, 1, 600, 500},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := d.AddRecessPolicy("ai", time.Saturday, c.ini, c.fim, c.minutos, c.vezes)
			if err == nil {
				t.Fatal("esperava erro, veio nil")
			}
		})
	}
}
