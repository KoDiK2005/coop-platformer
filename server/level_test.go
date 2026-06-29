package main

import "testing"

// TestGenerateLevelIsJumpable проверяет, что между каждой парой соседних
// платформ горизонтальный разрыв и перепад высоты остаются в пределах,
// прыгаемых при текущей физике клиента (см. константы maxSafeGap/maxSafeDY
// и комментарий про физику в level.go). Гоняем много сидов, потому что
// сегменты выбираются случайно — один прогон не покрыл бы все паттерны.
func TestGenerateLevelIsJumpable(t *testing.T) {
	const gapMargin = 20.0 // небольшой запас на неточность чекпоинт-гэпов

	for seed := int64(0); seed < 200; seed++ {
		level := generateLevel(seed)
		for i := 1; i < len(level.Platforms); i++ {
			prev := level.Platforms[i-1]
			cur := level.Platforms[i]

			gap := cur.X - (prev.X + prev.W)
			if gap > maxSafeGap+gapMargin {
				t.Fatalf("seed %d: непрыгаемый горизонтальный разрыв %.1f между платформами %d и %d", seed, gap, i-1, i)
			}

			dy := cur.Y - prev.Y
			if dy < 0 {
				dy = -dy
			}
			if dy > maxSafeDY+gapMargin {
				t.Fatalf("seed %d: непрыгаемый перепад высоты %.1f между платформами %d и %d", seed, dy, i-1, i)
			}
		}

		if len(level.Platforms) < 2 {
			t.Fatalf("seed %d: уровень слишком короткий (%d платформ)", seed, len(level.Platforms))
		}
	}
}

// TestGenerateLevelHasCheckpoints проверяет, что между секциями
// действительно расставляются чекпоинты, а не только старт и финиш.
func TestGenerateLevelHasCheckpoints(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		level := generateLevel(seed)
		checkpoints := 0
		for _, p := range level.Platforms {
			if p.Checkpoint {
				checkpoints++
			}
		}
		if checkpoints == 0 {
			t.Fatalf("seed %d: в уровне нет ни одного чекпоинта", seed)
		}
	}
}
