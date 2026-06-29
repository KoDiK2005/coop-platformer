package main

import "testing"

func TestAssignColor(t *testing.T) {
	tests := []struct {
		name     string
		used     []string
		expected string
	}{
		{"пустая комната — первый свободный цвет", nil, playerColors[0]},
		{"первый цвет занят", []string{playerColors[0]}, playerColors[1]},
		{"заняты первые два", []string{playerColors[0], playerColors[1]}, playerColors[2]},
		{"все цвета заняты — возвращаем первый по кругу", playerColors, playerColors[0]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Room{players: make(map[string]*Player)}
			for i, c := range tc.used {
				r.players[string(rune('a'+i))] = &Player{Color: c}
			}
			got := assignColor(r)
			if got != tc.expected {
				t.Errorf("ожидали %q, получили %q", tc.expected, got)
			}
		})
	}
}

func TestAllPlayersReady(t *testing.T) {
	tests := []struct {
		name     string
		ready    []bool // одна запись на игрока
		expected bool
	}{
		{"нет игроков", nil, false},
		{"один игрок, готов — всё равно мало", []bool{true}, false},
		{"двое, один не готов", []bool{true, false}, false},
		{"двое, оба готовы", []bool{true, true}, true},
		{"трое, оба готовы кроме одного", []bool{true, true, false}, false},
		{"четверо, все готовы", []bool{true, true, true, true}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			players := make(map[string]*Player, len(tc.ready))
			for i, ready := range tc.ready {
				players[string(rune('a'+i))] = &Player{Ready: ready}
			}
			got := allPlayersReady(players)
			if got != tc.expected {
				t.Errorf("ожидали %v, получили %v", tc.expected, got)
			}
		})
	}
}
