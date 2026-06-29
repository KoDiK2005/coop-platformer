package main

import "math/rand"

// Platform — прямоугольная платформа уровня в мировых координатах.
type Platform struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Level описывает один процедурно сгенерированный уровень.
type Level struct {
	Platforms []Platform `json:"platforms"`
	Start     Platform   `json:"start"` // координаты точки спавна (w/h не используются)
	Finish    Platform   `json:"finish"`
	Width     float64    `json:"width"`
	Height    float64    `json:"height"`
}

// generateLevel строит цепочку платформ со случайными, но проходимыми
// промежутками. Сложность (длина прыжка) растёт по ходу уровня.
func generateLevel(seed int64) Level {
	rng := rand.New(rand.NewSource(seed))

	const groundH = 40.0
	levelHeight := 720.0

	platforms := []Platform{
		// стартовая площадка
		{X: 0, Y: levelHeight - groundH, W: 360, H: groundH},
	}

	x := 360.0
	y := levelHeight - groundH
	const maxGapX = 140.0 // максимум, который игрок может перепрыгнуть
	const maxStepY = 90.0
	const minY = 180.0
	maxY := levelHeight - groundH

	steps := 16
	for i := 0; i < steps; i++ {
		gap := 70 + rng.Float64()*maxGapX*0.7
		w := 110 + rng.Float64()*120

		dy := (rng.Float64()*2 - 1) * maxStepY
		y += dy
		if y < minY {
			y = minY
		}
		if y > maxY {
			y = maxY
		}

		x += gap
		platforms = append(platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}

	// финишная платформа — шире и заметнее, чуть приподнята
	finishX := x + 100
	finishY := y - 20
	if finishY < minY {
		finishY = minY
	}
	finish := Platform{X: finishX, Y: finishY, W: 220, H: 28}
	platforms = append(platforms, finish)

	return Level{
		Platforms: platforms,
		Start:     Platform{X: 60, Y: levelHeight - groundH - 60},
		Finish:    finish,
		Width:     finishX + finish.W + 200,
		Height:    levelHeight,
	}
}
