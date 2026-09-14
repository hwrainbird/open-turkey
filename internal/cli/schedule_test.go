package cli

import (
	"testing"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
)

func TestParseDias(t *testing.T) {
	casos := []struct {
		entrada string
		espera  []time.Weekday
	}{
		{"mon", []time.Weekday{time.Monday}},
		{"mon,wed,fri", []time.Weekday{time.Monday, time.Wednesday, time.Friday}},
		{"mon-fri", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}},
		{"weekdays", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}},
		{"weekend", []time.Weekday{time.Sunday, time.Saturday}},
		{"MON", []time.Weekday{time.Monday}},
		{"seg", []time.Weekday{time.Monday}},
		// Intervalo que dá a volta na semana.
		{"fri-mon", []time.Weekday{time.Sunday, time.Monday, time.Friday, time.Saturday}},
		// Repetição não duplica.
		{"mon,mon,mon", []time.Weekday{time.Monday}},
	}

	for _, c := range casos {
		t.Run(c.entrada, func(t *testing.T) {
			got, err := ParseDias(c.entrada)
			if err != nil {
				t.Fatalf("ParseDias(%q): %v", c.entrada, err)
			}
			if len(got) != len(c.espera) {
				t.Fatalf("ParseDias(%q) = %v, queria %v", c.entrada, got, c.espera)
			}
			for i := range got {
				if got[i] != c.espera[i] {
					t.Fatalf("ParseDias(%q) = %v, queria %v", c.entrada, got, c.espera)
				}
			}
		})
	}
}

func TestParseDiasRecusaLixo(t *testing.T) {
	for _, entrada := range []string{"", "funday", "mon-funday", "13"} {
		if _, err := ParseDias(entrada); err == nil {
			t.Fatalf("ParseDias(%q) deveria ter falhado", entrada)
		}
	}
}

func TestParseJanela(t *testing.T) {
	inicio, fim, err := ParseJanela("17:30-24:00")
	if err != nil {
		t.Fatalf("ParseJanela: %v", err)
	}
	if inicio != 17*60+30 {
		t.Fatalf("início = %d, queria %d", inicio, 17*60+30)
	}
	if fim != db.MinutosNoDia {
		t.Fatalf("fim = %d, queria %d", fim, db.MinutosNoDia)
	}
}

func TestParseJanelaRecusaLixo(t *testing.T) {
	casos := []string{
		"17:30",       // sem fim
		"25:00-26:00", // fora do dia
		"10:60-11:00", // minuto inválido
		"24:30-24:40", // 24h só existe como 24:00
		"18:00-09:00", // termina antes de começar
		"18:00-18:00", // vazia
	}
	for _, c := range casos {
		if _, _, err := ParseJanela(c); err == nil {
			t.Fatalf("ParseJanela(%q) deveria ter falhado", c)
		}
	}
}

// O complemento é o que transforma "liberado 10:00–10:05" em duas janelas de
// bloqueio cobrindo o resto do dia.
func TestComplemento(t *testing.T) {
	janelas := Complemento(10*60, 10*60+5)
	if len(janelas) != 2 {
		t.Fatalf("queria 2 janelas, veio %d: %v", len(janelas), janelas)
	}
	if janelas[0] != [2]int{0, 600} {
		t.Fatalf("primeira janela = %v, queria [0 600]", janelas[0])
	}
	if janelas[1] != [2]int{605, db.MinutosNoDia} {
		t.Fatalf("segunda janela = %v, queria [605 %d]", janelas[1], db.MinutosNoDia)
	}
}

// Uma folga colada na meia-noite não deve gerar uma janela de tamanho zero.
func TestComplementoNasPontas(t *testing.T) {
	if got := Complemento(0, 600); len(got) != 1 || got[0] != [2]int{600, db.MinutosNoDia} {
		t.Fatalf("folga no início do dia: veio %v", got)
	}
	if got := Complemento(600, db.MinutosNoDia); len(got) != 1 || got[0] != [2]int{0, 600} {
		t.Fatalf("folga no fim do dia: veio %v", got)
	}
	if got := Complemento(0, db.MinutosNoDia); len(got) != 0 {
		t.Fatalf("folga o dia inteiro não deveria gerar janela: veio %v", got)
	}
}
