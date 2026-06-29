package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

const maxPlayers = 4
const minPlayersToStart = 2
const countdownSeconds = 5

var playerColors = []string{"#ff5d5d", "#5da9ff", "#5dff8a", "#ffd25d"}

type RoomState string

const (
	StateWaiting  RoomState = "waiting"  // лобби: игроки выставляют готовность
	StatePlaying  RoomState = "playing"  // раунд идёт
	StateFinished RoomState = "finished" // победа, идёт пауза перед возвратом в лобби
)

// Player — состояние одного подключённого игрока с точки зрения сервера.
// Позиция приходит от клиента (клиент авторитативен по физике — это
// кооп-игра без анти-чита, упрощение оправдано масштабом проекта).
type Player struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	Facing   int     `json:"facing"`
	OnGround bool    `json:"onGround"`
	AtFinish bool    `json:"atFinish"`
	Ready    bool    `json:"ready"`

	conn *Conn
}

// Room — единственная игровая комната (до 4 игроков). Игроки, подключившиеся
// во время идущего раунда, попадают в очередь зрителей и становятся
// участниками со следующего раунда.
type Room struct {
	mu sync.Mutex

	state RoomState

	players map[string]*Player
	queue   []*Player

	level     Level
	fallCount int
	lastHint  time.Time

	counting     bool
	countdownGen int
}

var room = &Room{players: make(map[string]*Player), state: StateWaiting}

type inMessage struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	Facing   int     `json:"facing"`
	OnGround bool    `json:"onGround"`
	AtFinish bool    `json:"atFinish"`
	Fell     bool    `json:"fell"`
}

func main() {
	addr := ":8080"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWS)
	mux.Handle("/", http.FileServer(http.Dir("../frontend")))

	log.Printf("Сервер запущен: http://localhost%s (откройте этот адрес в браузере)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrade(w, r)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "Игрок"
	}

	room.mu.Lock()
	if len(room.players)+len(room.queue) >= maxPlayers {
		room.mu.Unlock()
		_ = conn.WriteMessage(mustJSON(map[string]any{"type": "full"}))
		conn.Close()
		return
	}

	id := randomID()
	p := &Player{ID: id, Name: name, conn: conn}

	spectating := room.state != StateWaiting
	if spectating {
		room.queue = append(room.queue, p)
	} else {
		p.Color = assignColor(room)
		room.players[id] = p
	}
	room.mu.Unlock()

	log.Printf("игрок %s (%s) подключился, режим=%s", id, name, room.state)

	broadcastLobby()
	readLoop(conn, id)
}

func assignColor(r *Room) string {
	used := make(map[string]bool, len(r.players))
	for _, p := range r.players {
		used[p.Color] = true
	}
	for _, c := range playerColors {
		if !used[c] {
			return c
		}
	}
	return playerColors[0]
}

func readLoop(conn *Conn, id string) {
	defer handleDisconnect(id)

	for {
		data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg inMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		handleClientMessage(id, msg)
	}
}

func handleClientMessage(id string, msg inMessage) {
	switch msg.Type {
	case "ready":
		handleReady(id)
	case "move":
		handleMove(id, msg)
	}
}

func handleReady(id string) {
	room.mu.Lock()
	p, ok := room.players[id]
	if !ok || room.state != StateWaiting {
		room.mu.Unlock()
		return
	}
	p.Ready = !p.Ready
	ready := p.Ready
	room.mu.Unlock()

	log.Printf("игрок %s готовность=%v", id, ready)
	reevaluateCountdown()
	broadcastLobby()
}

// reevaluateCountdown пересчитывает, должен ли идти обратный отсчёт, и
// запускает/отменяет его. Нужно звать при любом изменении состава или
// готовности игроков в лобби (ready-тоггл, отключение во время отсчёта) —
// иначе отсчёт может продолжаться для уже опустевшей комнаты и она
// застрянет в "playing" с нулём игроков навсегда.
func reevaluateCountdown() {
	room.mu.Lock()
	if room.state != StateWaiting {
		room.mu.Unlock()
		return
	}

	allReady := len(room.players) >= minPlayersToStart
	for _, other := range room.players {
		if !other.Ready {
			allReady = false
			break
		}
	}

	switch {
	case allReady && !room.counting:
		room.counting = true
		gen := room.countdownGen
		playerCount := len(room.players)
		room.mu.Unlock()
		log.Printf("все готовы (%d игроков) — запускаю отсчёт", playerCount)
		go runCountdown(gen)
		return
	case !allReady && room.counting:
		room.countdownGen++
		room.counting = false
		room.mu.Unlock()
		log.Println("отсчёт отменён — не все готовы или игрок вышел")
		broadcastAll(mustJSON(map[string]any{"type": "countdown", "seconds": 0}))
		return
	}
	room.mu.Unlock()
}

func runCountdown(gen int) {
	for s := countdownSeconds; s >= 1; s-- {
		room.mu.Lock()
		cancelled := room.countdownGen != gen
		room.mu.Unlock()
		if cancelled {
			return
		}
		broadcastAll(mustJSON(map[string]any{"type": "countdown", "seconds": s}))
		time.Sleep(1 * time.Second)
	}

	room.mu.Lock()
	if room.countdownGen != gen {
		room.mu.Unlock()
		return
	}
	if len(room.players) < minPlayersToStart {
		// все нужные игроки отвалились в последний момент — отменяем тихо,
		// без перехода в playing с пустой/неполной комнатой.
		room.counting = false
		room.countdownGen++
		room.mu.Unlock()
		broadcastAll(mustJSON(map[string]any{"type": "countdown", "seconds": 0}))
		broadcastLobby()
		return
	}
	room.level = generateLevel(time.Now().UnixNano())
	room.state = StatePlaying
	room.counting = false
	room.fallCount = 0
	room.lastHint = time.Time{}
	for _, p := range room.players {
		p.X, p.Y = room.level.Start.X, room.level.Start.Y
		p.VX, p.VY = 0, 0
		p.OnGround = true
		p.AtFinish = false
		p.Ready = false
	}
	level := room.level
	players := snapshotPlayers(room)
	room.mu.Unlock()

	log.Printf("раунд стартовал, игроков: %d", len(players))
	broadcastAll(mustJSON(map[string]any{"type": "countdown", "seconds": 0}))
	broadcastToPlayers(mustJSON(map[string]any{
		"type":    "start",
		"level":   level,
		"players": players,
	}))
	broadcastLobby()
}

func handleMove(id string, msg inMessage) {
	room.mu.Lock()
	if room.state != StatePlaying {
		room.mu.Unlock()
		return
	}
	p, ok := room.players[id]
	if !ok {
		room.mu.Unlock()
		return
	}

	p.X, p.Y, p.VX, p.VY, p.Facing, p.OnGround = msg.X, msg.Y, msg.VX, msg.VY, msg.Facing, msg.OnGround
	p.AtFinish = msg.AtFinish
	if msg.Fell {
		room.fallCount++
	}

	allAtFinish := len(room.players) > 0
	for _, other := range room.players {
		if !other.AtFinish {
			allAtFinish = false
			break
		}
	}

	justWon := false
	if allAtFinish {
		room.state = StateFinished
		justWon = true
	}

	shouldHint := false
	if !justWon && room.fallCount > 0 && room.fallCount%3 == 0 && time.Since(room.lastHint) > 20*time.Second {
		shouldHint = true
		room.lastHint = time.Now()
	}
	fallCount := room.fallCount
	room.mu.Unlock()

	broadcastToPlayersExcept("", mustJSON(map[string]any{
		"type":     "move",
		"id":       p.ID,
		"name":     p.Name,
		"x":        p.X,
		"y":        p.Y,
		"vx":       p.VX,
		"vy":       p.VY,
		"facing":   p.Facing,
		"onGround": p.OnGround,
		"atFinish": p.AtFinish,
	}))

	if justWon {
		broadcastToPlayers(mustJSON(map[string]any{"type": "win"}))
		go finishRoundAfterDelay()
	} else if shouldHint {
		go sendAIHint(fallCount)
	}
}

// finishRoundAfterDelay даёт игрокам полюбоваться победой, затем переводит
// комнату обратно в лобби, подмешивая зрителей из очереди в новый раунд.
func finishRoundAfterDelay() {
	time.Sleep(6 * time.Second)

	room.mu.Lock()
	if room.state != StateFinished {
		room.mu.Unlock()
		return
	}
	room.state = StateWaiting
	for _, p := range room.players {
		p.Ready = false
		p.AtFinish = false
	}
	for _, p := range room.queue {
		p.Color = assignColor(room)
		p.Ready = false
		room.players[p.ID] = p
	}
	room.queue = nil
	room.mu.Unlock()

	broadcastLobby()
}

func handleDisconnect(id string) {
	room.mu.Lock()
	delete(room.players, id)
	for i, p := range room.queue {
		if p.ID == id {
			room.queue = append(room.queue[:i], room.queue[i+1:]...)
			break
		}
	}

	if len(room.players) == 0 && room.state != StateWaiting {
		room.state = StateWaiting
		room.counting = false
		room.countdownGen++
		for _, p := range room.queue {
			p.Color = assignColor(room)
			p.Ready = false
			room.players[p.ID] = p
		}
		room.queue = nil
	}
	room.mu.Unlock()

	log.Printf("игрок %s отключился", id)
	reevaluateCountdown()
	broadcastLobby()
}

// broadcastAll шлёт сырое сообщение абсолютно всем подключённым (игрокам и
// зрителям в очереди) — используется для countdown, который должны видеть все.
func broadcastAll(data []byte) {
	room.mu.Lock()
	targets := make([]*Player, 0, len(room.players)+len(room.queue))
	for _, p := range room.players {
		targets = append(targets, p)
	}
	targets = append(targets, room.queue...)
	room.mu.Unlock()

	for _, p := range targets {
		_ = p.conn.WriteMessage(data)
	}
}

func broadcastToPlayers(data []byte) {
	broadcastToPlayersExcept("", data)
}

func broadcastToPlayersExcept(excludeID string, data []byte) {
	room.mu.Lock()
	targets := make([]*Player, 0, len(room.players))
	for _, p := range room.players {
		if p.ID != excludeID {
			targets = append(targets, p)
		}
	}
	room.mu.Unlock()

	for _, p := range targets {
		_ = p.conn.WriteMessage(data)
	}
}

// broadcastLobby отправляет каждому подключённому персонализированный снимок
// лобби (его собственный id и флаг "я зритель"), чтобы клиент знал, что рисовать.
func broadcastLobby() {
	room.mu.Lock()
	players := snapshotPlayers(room)
	state := room.state
	queue := append([]*Player{}, room.queue...)
	room.mu.Unlock()

	type lobbyPlayer struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Ready bool   `json:"ready"`
	}
	list := make([]lobbyPlayer, 0, len(players))
	for _, p := range players {
		list = append(list, lobbyPlayer{p.ID, p.Name, p.Color, p.Ready})
	}

	send := func(p *Player, spectating bool) {
		_ = p.conn.WriteMessage(mustJSON(map[string]any{
			"type":       "lobby",
			"state":      state,
			"yourId":     p.ID,
			"spectating": spectating,
			"players":    list,
			"queueCount": len(queue),
		}))
	}
	for _, p := range players {
		send(p, false)
	}
	for _, p := range queue {
		send(p, true)
	}
}

func snapshotPlayers(r *Room) []*Player {
	list := make([]*Player, 0, len(r.players))
	for _, p := range r.players {
		list = append(list, p)
	}
	return list
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		var buf bytes.Buffer
		buf.WriteString(`{"type":"error"}`)
		return buf.Bytes()
	}
	return b
}

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomID() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = idAlphabet[rand.Intn(len(idAlphabet))]
	}
	return string(b)
}
