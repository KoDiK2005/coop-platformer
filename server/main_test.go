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
