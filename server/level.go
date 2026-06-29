package main

import "math/rand"

// Platform — прямоугольная платформа уровня в мировых координатах.
// Checkpoint отмечает платформы-чекпоинты: коснувшись такой, игрок дальше
// возрождается при падении с неё, а не с самого начала уровня.
type Platform struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	Checkpoint bool    `json:"checkpoint,omitempty"`
}

// Level описывает один процедурно сгенерированный уровень.
type Level struct {
	Platforms []Platform `json:"platforms"`
	Start     Platform   `json:"start"` // координаты точки спавна (w/h не используются)
	Finish    Platform   `json:"finish"`
	Width     float64    `json:"width"`
	Height    float64    `json:"height"`
}

const (
	levelHeight = 720.0
	groundH     = 40.0
	minPlatY    = 180.0
	maxPlatY    = levelHeight - groundH

	// Прыжок (см. JUMP_VELOCITY/GRAVITY/MOVE_SPEED в frontend/game.js):
	// при текущих константах максимальная высота прыжка ≈117px, а
	// безопасная горизонтальная дальность (с разбега) ≈210px. Все паттерны
	// ниже держат гэпы и перепады в этих пределах, чтобы уровень был
	// проходим без двойных прыжков.
	maxSafeGap = 200.0
	maxSafeDY  = 100.0
)

// segment — функция, генерирующая один "кусок" уровня определённого стиля
// начиная от (x, y), дописывает платформы в platforms и возвращает
// координату, от которой нужно продолжать следующий сегмент.
type segment func(rng *rand.Rand, x, y float64, platforms *[]Platform) (nextX, nextY float64)

// segGaps — обычная секция: умеренные гэпы, умеренный разброс по высоте.
func segGaps(rng *rand.Rand, x, y float64, platforms *[]Platform) (float64, float64) {
	count := 3 + rng.Intn(3)
	for i := 0; i < count; i++ {
		gap := 70 + rng.Float64()*120
		w := 110 + rng.Float64()*120
		y = clampY(y + (rng.Float64()*2-1)*80)
		x += gap
		*platforms = append(*platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}
	return x, y
}

// segStairs — лестница: несколько платформ подряд, стабильно поднимающихся
// или спускающихся в одном направлении, с короткими прыжками между ними.
func segStairs(rng *rand.Rand, x, y float64, platforms *[]Platform) (float64, float64) {
	dir := 1.0
	if rng.Intn(2) == 0 {
		dir = -1
	}
	step := 45 + rng.Float64()*30
	count := 4 + rng.Intn(3)
	for i := 0; i < count; i++ {
		gap := 55 + rng.Float64()*40
		w := 100 + rng.Float64()*50
		y = clampY(y + dir*step)
		x += gap
		*platforms = append(*platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}
	return x, y
}

// segFloatingIslands — широкие платформы, далеко друг от друга и на разной
// высоте: ощущение прыжков "по островам" с заметным риском промахнуться.
func segFloatingIslands(rng *rand.Rand, x, y float64, platforms *[]Platform) (float64, float64) {
	count := 3 + rng.Intn(2)
	for i := 0; i < count; i++ {
		gap := 130 + rng.Float64()*(maxSafeGap-130)
		w := 170 + rng.Float64()*110
		y = clampY(y + (rng.Float64()*2-1)*maxSafeDY)
		x += gap
		*platforms = append(*platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}
	return x, y
}

// segNarrowBridge — серия коротких узких платформ на одной высоте: секция
// на точность движения, а не на высоту прыжка.
func segNarrowBridge(rng *rand.Rand, x, y float64, platforms *[]Platform) (float64, float64) {
	count := 4 + rng.Intn(3)
	for i := 0; i < count; i++ {
		gap := 45 + rng.Float64()*35
		w := 65 + rng.Float64()*35
		x += gap
		*platforms = append(*platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}
	return x, y
}

// segZigzag — резкие чередующиеся скачки вверх-вниз.
func segZigzag(rng *rand.Rand, x, y float64, platforms *[]Platform) (float64, float64) {
	count := 4 + rng.Intn(3)
	sign := 1.0
	for i := 0; i < count; i++ {
		gap := 75 + rng.Float64()*70
		w := 95 + rng.Float64()*70
		y = clampY(y + sign*(55+rng.Float64()*40))
		sign = -sign
		x += gap
		*platforms = append(*platforms, Platform{X: x, Y: y, W: w, H: 24})
		x += w
	}
	return x, y
}

func clampY(y float64) float64 {
	if y < minPlatY {
		return minPlatY
	}
	if y > maxPlatY {
		return maxPlatY
	}
	return y
}

var allSegments = []segment{segGaps, segStairs, segFloatingIslands, segNarrowBridge, segZigzag}

// generateLevel случайно комбинирует несколько стилевых секций (лестницы,
// острова, узкие мостики, зигзаги, обычные гэпы) и расставляет чекпоинты
// между ними — поэтому каждый раунд уровень не только генерируется заново,
// но и структурно ощущается по-другому.
func generateLevel(seed int64) Level {
	rng := rand.New(rand.NewSource(seed))

	platforms := []Platform{
		{X: 0, Y: maxPlatY, W: 360, H: groundH},
	}

	x, y := 360.0, maxPlatY

	segmentCount := 5 + rng.Intn(2)
	for i := 0; i < segmentCount; i++ {
		seg := allSegments[rng.Intn(len(allSegments))]
		x, y = seg(rng, x, y, &platforms)

		// чекпоинт между секциями (кроме самой последней — там финиш)
		if i < segmentCount-1 {
			gap := 60 + rng.Float64()*40
			x += gap
			checkpoint := Platform{X: x, Y: y, W: 130, H: 26, Checkpoint: true}
			platforms = append(platforms, checkpoint)
			x += checkpoint.W
		}
	}

	finishX := x + 100
	finishY := clampY(y - 20)
	finish := Platform{X: finishX, Y: finishY, W: 220, H: 28}
	platforms = append(platforms, finish)

	return Level{
		Platforms: platforms,
		Start:     Platform{X: 60, Y: maxPlatY - 60},
		Finish:    finish,
		Width:     finishX + finish.W + 200,
		Height:    levelHeight,
	}
}
