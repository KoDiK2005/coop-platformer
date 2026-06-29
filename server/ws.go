package main

// Минимальная реализация серверной части протокола WebSocket (RFC 6455)
// на стандартной библиотеке, без сторонних зависимостей.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const wsMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// Conn оборачивает поднятое (hijacked) TCP-соединение и умеет читать/писать
// неразбитые (unfragmented) WebSocket-фреймы — этого достаточно для коротких
// JSON-сообщений, которыми обмениваются клиент и сервер этой игры.
//
// На одно соединение пишут разные горутины (например, и обработчик
// "ready", и таймер обратного отсчёта почти одновременно рассылают своё
// сообщение). Без writeMu их байты могут переплестись в один TCP-сегмент и
// сломать структуру WebSocket-фрейма — клиент увидит это как мгновенный
// разрыв соединения (код 1006), что и выглядело как "комната не отвечает".
type Conn struct {
	rw      *bufio.ReadWriter
	conn    net.Conn
	writeMu sync.Mutex
}

var errUnsupportedFrame = errors.New("ws: фрагментированные или управляющие фреймы такого типа не поддерживаются")

// Upgrade выполняет handshake и возвращает обёртку Conn для дальнейшего
// чтения/записи фреймов.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("ws: отсутствует заголовок Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return nil, errors.New("ws: http.Hijacker не поддерживается")
	}

	netConn, buf, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	accept := computeAccept(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"

	if _, err := buf.WriteString(resp); err != nil {
		netConn.Close()
		return nil, err
	}
	if err := buf.Flush(); err != nil {
		netConn.Close()
		return nil, err
	}

	return &Conn{rw: buf, conn: netConn}, nil
}

func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsMagicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ReadMessage блокирующе читает один текстовый фрейм и возвращает его payload.
// Ping отвечает Pong автоматически и переходит к следующему фрейму.
func (c *Conn) ReadMessage() ([]byte, error) {
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		if !fin {
			return nil, errUnsupportedFrame
		}
		switch opcode {
		case opText, opBinary:
			return payload, nil
		case opClose:
			c.WriteClose()
			return nil, io.EOF
		case opPing:
			_ = c.writeFrame(opPong, payload)
		case opPong:
			// игнорируем
		default:
			return nil, errUnsupportedFrame
		}
	}
}

func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err = io.ReadFull(c.rw, header); err != nil {
		return false, 0, nil, err
	}
	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0F
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)

	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c.rw, ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c.rw, ext); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(c.rw, maskKey[:]); err != nil {
			return false, 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.rw, payload); err != nil {
		return false, 0, nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return fin, opcode, payload, nil
}

// WriteMessage отправляет один немаскированный текстовый фрейм (как требует
// протокол для серверной стороны).
func (c *Conn) WriteMessage(data []byte) error {
	return c.writeFrame(opText, data)
}

func (c *Conn) WriteClose() error {
	return c.writeFrame(opClose, nil)
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	length := len(payload)
	header := []byte{0x80 | opcode} // FIN=1, без фрагментации

	switch {
	case length <= 125:
		header = append(header, byte(length))
	case length <= 65535:
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(length))
		header = append(header, 126)
		header = append(header, ext...)
	default:
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(length))
		header = append(header, 127)
		header = append(header, ext...)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := c.rw.Write(header); err != nil {
		return err
	}
	if _, err := c.rw.Write(payload); err != nil {
		return err
	}
	return c.rw.Flush()
}

func (c *Conn) Close() error {
	return c.conn.Close()
}
