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

var playerColors = []string{"#ff5d5d", "#5da9ff", "#5dff8a", "#ffd25d"}

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
	AtFinish bool    `json:"atFinish"`

	conn *Conn
}

// Room — единственная игровая комната (до 4 игроков), создаётся автоматически
// при первом подключении и пересоздаётся (новый уровень), когда опустеет.
type Room struct {
	mu        sync.Mutex
	players   map[string]*Player
	level     Level
	fallCount int
	lastHint  time.Time
	won       bool
}

var room = &Room{players: make(map[string]*Player)}

type inMessage struct {
	Type string  `json:"type"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	VX   float64 `json:"vx"`
	VY   float64 `json:"vy"`
	Facing   int  `json:"facing"`
	AtFinish bool `json:"atFinish"`
	Fell     bool `json:"fell"`
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

	room.mu.Lock()
	if len(room.players) >= maxPlayers {
		room.mu.Unlock()
		_ = conn.WriteMessage(mustJSON(map[string]any{"type": "full"}))
		conn.Close()
		return
	}
	if len(room.players) == 0 {
		room.level = generateLevel(time.Now().UnixNano())
		room.fallCount = 0
		room.won = false
	}

	id := randomID()
	color := playerColors[len(room.players)]
	p := &Player{
		ID:    id,
		Name:  "Игрок",
		Color: color,
		X:     room.level.Start.X,
		Y:     room.level.Start.Y,
		conn:  conn,
	}
	room.players[id] = p

	others := snapshotPlayers(room)
	room.mu.Unlock()

	log.Printf("игрок %s подключился (%d/%d)", id, len(others), maxPlayers)

	_ = conn.WriteMessage(mustJSON(map[string]any{
		"type":    "init",
		"id":      id,
		"color":   color,
		"level":   room.level,
		"players": others,
	}))

	broadcastExcept(id, mustJSON(map[string]any{
		"type":   "join",
		"player": p,
	}))

	readLoop(conn, id)
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
	room.mu.Lock()
	p, ok := room.players[id]
	if !ok {
		room.mu.Unlock()
		return
	}

	switch msg.Type {
	case "move":
		p.X, p.Y, p.VX, p.VY, p.Facing = msg.X, msg.Y, msg.VX, msg.VY, msg.Facing
		if msg.Name != "" {
			p.Name = msg.Name
		}
		p.AtFinish = msg.AtFinish
		if msg.Fell {
			room.fallCount++
		}
	}

	allAtFinish := len(room.players) > 0 && !room.won
	for _, other := range room.players {
		if !other.AtFinish {
			allAtFinish = false
			break
		}
	}
	if allAtFinish {
		room.won = true
	}

	shouldHint := false
	if !room.won && room.fallCount > 0 && room.fallCount%3 == 0 && time.Since(room.lastHint) > 20*time.Second {
		shouldHint = true
		room.lastHint = time.Now()
	}
	fallCount := room.fallCount
	room.mu.Unlock()

	broadcastExcept("", mustJSON(map[string]any{
		"type":     "move",
		"id":       p.ID,
		"name":     p.Name,
		"x":        p.X,
		"y":        p.Y,
		"vx":       p.VX,
		"vy":       p.VY,
		"facing":   p.Facing,
		"atFinish": p.AtFinish,
	}))

	if allAtFinish {
		broadcastExcept("", mustJSON(map[string]any{"type": "win"}))
	}

	if shouldHint {
		go sendAIHint(fallCount)
	}
}

func handleDisconnect(id string) {
	room.mu.Lock()
	delete(room.players, id)
	empty := len(room.players) == 0
	room.mu.Unlock()

	log.Printf("игрок %s отключился", id)
	broadcastExcept(id, mustJSON(map[string]any{"type": "leave", "id": id}))

	if empty {
		log.Println("комната пуста — уровень будет пересоздан для следующей сессии")
	}
}

func broadcastExcept(excludeID string, data []byte) {
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
