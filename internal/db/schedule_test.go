package db

import (
	"path/filepath"
	"testing"
	"time"
)

// seg às 18:00 — dentro de uma janela "seg 17:30–24:00".
func segAs(hora, minuto int) time.Time {
	// 2026-09-14 é uma segunda-feira.
	return time.Date(2026, 9, 14, hora, minuto, 0, 0, time.UTC)
}

func sabAs(hora, minuto int) time.Time {
	// 2026-09-19 é um sábado.
	return time.Date(2026, 9, 19, hora, minuto, 0, 0, time.UTC)
}

func TestJanelaAtual(t *testing.T) {
	noite := Schedule{Weekday: time.Monday, StartMin: 17*60 + 30, EndMin: MinutosNoDia}

	casos := []struct {
		nome   string
		agora  time.Time
		espera bool
	}{
		{"antes da janela", segAs(17, 29), false},
		{"no minuto de abertura", segAs(17, 30), true},
		{"no meio", segAs(21, 0), true},
		{"último minuto do dia", segAs(23, 59), true},
		{"outro dia da semana", sabAs(21, 0), false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := DentroDaJanela(c.agora, []Schedule{noite}); got != c.espera {
				t.Fatalf("DentroDaJanela = %v, queria %v", got, c.espera)
			}
		})
	}
}

// O fim exclusivo importa: uma janela que acaba às 10:05 não pode incluir
// 10:05, senão a folga combinada some.
func TestFimEhExclusivo(t *testing.T) {
	folga := Schedule{Weekday: time.Saturday, StartMin: 10 * 60, EndMin: 10*60 + 5}

	if !DentroDaJanela(sabAs(10, 4), []Schedule{folga}) {
		t.Fatal("10:04 deveria estar dentro de 10:00–10:05")
	}
	if DentroDaJanela(sabAs(10, 5), []Schedule{folga}) {
		t.Fatal("10:05 não deveria estar dentro de 10:00–10:05")
	}
}

// Janelas podem se sobrepor; vale a que termina mais tarde, porque é ela que
// decide até quando o bloco fica de pé.
func TestSobreposicaoUsaOFimMaisDistante(t *testing.T) {
	curta := Schedule{Weekday: time.Monday, StartMin: 9 * 60, EndMin: 10 * 60}
	longa := Schedule{Weekday: time.Monday, StartMin: 9 * 60, EndMin: 18 * 60}

	fim := FimDaJanela(segAs(9, 30), []Schedule{curta, longa})
	if fim.Hour() != 18 {
		t.Fatalf("fim = %v, queria 18:00", fim)
	}
}

func TestFimDaJanelaForaDeQualquerJanela(t *testing.T) {
	janela := Schedule{Weekday: time.Monday, StartMin: 9 * 60, EndMin: 10 * 60}
	agora := segAs(15, 0)

	if fim := FimDaJanela(agora, []Schedule{janela}); !fim.Equal(agora) {
		t.Fatalf("fora de janela, FimDaJanela deveria devolver o próprio instante; deu %v", fim)
	}
}

// --------------------------------------------------------------------------
// Reconciliação
// --------------------------------------------------------------------------

func bancoDeTeste(t *testing.T) *DB {
	t.Helper()
	d, err := OpenDB(filepath.Join(t.TempDir(), "teste.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func blocoAgendado(t *testing.T, d *DB, nome string) {
	t.Helper()
	if err := d.CreateBlock(nome); err != nil {
		t.Fatalf("CreateBlock: %v", err)
	}
	if err := d.AddSites(nome, []string{"exemplo.com"}); err != nil {
		t.Fatalf("AddSites: %v", err)
	}
}

func TestReconcileLigaEDesligaNaJanela(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "foco")

	if err := d.AddSchedule("foco", time.Monday, 17*60+30, MinutosNoDia, true, 300); err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}

	// Antes da janela: nada acontece.
	if _, err := d.ReconcileSchedules(segAs(17, 0)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("foco"); ativo {
		t.Fatal("bloco não deveria estar ativo antes da janela")
	}

	// Janela aberta: liga, e travado.
	if _, err := d.ReconcileSchedules(segAs(18, 0)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("foco"); !ativo {
		t.Fatal("bloco deveria estar ativo dentro da janela")
	}
	if travado, _ := d.IsBlockLocked("foco"); !travado {
		t.Fatal("bloco agendado com trava deveria estar travado")
	}

	// Janela fechada (terça de manhã): desliga sozinho.
	terca := segAs(9, 0).AddDate(0, 0, 1)
	if _, err := d.ReconcileSchedules(terca); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("foco"); ativo {
		t.Fatal("bloco deveria ter sido desligado ao fechar a janela")
	}
}

// Um bloco ligado na mão não é desligado pela agenda: quem ligou
// explicitamente é quem decide desligar.
func TestReconcileNaoDesligaBlocoManual(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "manual")

	if err := d.AddSchedule("manual", time.Monday, 9*60, 10*60, false, 0); err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}
	if err := d.ActivateBlock("manual", false, 0); err != nil {
		t.Fatalf("ActivateBlock: %v", err)
	}

	// Muito depois da janela.
	if _, err := d.ReconcileSchedules(segAs(23, 0)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("manual"); !ativo {
		t.Fatal("a agenda não pode desligar um bloco que foi ligado na mão")
	}
}

// Este é o teste que importa para o desafio valer alguma coisa: destravar no
// meio de uma janela não pode ser desfeito pelo daemon cinco segundos depois.
func TestSupressaoImpedeReligarNaMesmaJanela(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "ia")

	// Agenda diária, que é o caso real: "bloqueado o tempo todo". Com uma
	// janela só na segunda, não haveria nada para religar na terça.
	for dia := time.Sunday; dia <= time.Saturday; dia++ {
		if err := d.AddSchedule("ia", dia, 0, MinutosNoDia, true, 300); err != nil {
			t.Fatalf("AddSchedule: %v", err)
		}
	}

	if _, err := d.ReconcileSchedules(segAs(8, 0)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("ia"); !ativo {
		t.Fatal("bloco deveria ter ligado")
	}

	// O usuário passa no desafio e desliga; o CLI suprime até o fim da janela.
	if err := d.DeactivateBlock("ia"); err != nil {
		t.Fatalf("DeactivateBlock: %v", err)
	}
	janelas, _ := d.GetSchedulesByName("ia")
	if err := d.SetSuppression("ia", FimDaJanela(segAs(8, 0), janelas)); err != nil {
		t.Fatalf("SetSuppression: %v", err)
	}

	// Mesma janela, mais tarde: continua desligado.
	if _, err := d.ReconcileSchedules(segAs(20, 0)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("ia"); ativo {
		t.Fatal("a supressão deveria impedir o religamento na mesma janela")
	}

	// Dia seguinte: a janela nova volta a valer.
	terca := segAs(8, 0).AddDate(0, 0, 1)
	if _, err := d.ReconcileSchedules(terca); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if ativo, _ := d.IsBlockActive("ia"); !ativo {
		t.Fatal("a janela do dia seguinte deveria religar o bloco")
	}
}

func TestAddScheduleRecusaJanelaInvalida(t *testing.T) {
	d := bancoDeTeste(t)
	blocoAgendado(t, d, "x")

	casos := []struct {
		nome        string
		dia         time.Weekday
		inicio, fim int
	}{
		{"fim antes do início", time.Monday, 600, 500},
		{"fim igual ao início", time.Monday, 600, 600},
		{"passa da meia-noite", time.Monday, 600, MinutosNoDia + 1},
		{"dia inválido", time.Weekday(9), 0, 60},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if err := d.AddSchedule("x", c.dia, c.inicio, c.fim, true, 300); err == nil {
				t.Fatal("esperava erro, veio nil")
			}
		})
	}
}
