package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// AI-рассказчик: когда команда часто срывается с платформ, сервер просит
// локальную модель Ollama придумать короткую подсказку/реплику и разослать
// её всем игрокам. Это даёт реальную ценность (живой динамический комментарий
// вместо одной и той же статичной надписи), при этом полностью необязательно —
// если Ollama не запущена, игра просто использует одну из заготовленных фраз.

var ollamaHost = envOr("OLLAMA_HOST", "http://localhost:11434")
var ollamaModel = envOr("OLLAMA_MODEL", "llama3")

var fallbackHints = []string{
	"Падать — это часть пути. Попробуйте прыгать чуть раньше края платформы!",
	"Команда, не сдавайтесь — синхронизируйте прыжки и ищите ритм уровня.",
	"Совет: разбег перед прыжком даёт больше дальности.",
	"Иногда лучше подождать друга на платформе, чем прыгать в одиночку.",
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

func sendAIHint(fallCount int) {
	text := requestHintFromOllama(fallCount)
	broadcastToPlayers(mustJSON(map[string]any{
		"type": "hint",
		"text": text,
	}))
}

func requestHintFromOllama(fallCount int) string {
	prompt := "Ты — весёлый рассказчик в кооперативном 2D-платформере для 2-4 игроков. " +
		"Команда уже упала с платформ суммарно " + strconv.Itoa(fallCount) + " раз. " +
		"Дай одну короткую (до 18 слов), дружелюбную и слегка ироничную подсказку или фразу поддержки на русском языке. " +
		"Без markdown, без кавычек, только сама фраза."

	reqBody, _ := json.Marshal(ollamaRequest{Model: ollamaModel, Prompt: prompt, Stream: false})

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Post(ollamaHost+"/api/generate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Println("ollama недоступна, использую заготовленную подсказку:", err)
		return pickFallback(fallCount)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return pickFallback(fallCount)
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Response == "" {
		return pickFallback(fallCount)
	}

	return out.Response
}

func pickFallback(fallCount int) string {
	return fallbackHints[fallCount%len(fallbackHints)]
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
