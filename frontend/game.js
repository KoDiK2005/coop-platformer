// Кооп-платформер: клиентская физика, рендер на Canvas, синхронизация по WebSocket.

const canvas = document.getElementById("game");
const ctx = canvas.getContext("2d");
const lobby = document.getElementById("lobby");
const nameInput = document.getElementById("nameInput");
const joinBtn = document.getElementById("joinBtn");
const statusEl = document.getElementById("status");
const hud = document.getElementById("hud");

const GRAVITY = 1800;
const MOVE_SPEED = 320;
const JUMP_VELOCITY = -650;
const PLAYER_W = 28;
const PLAYER_H = 38;
const SEND_INTERVAL_MS = 50;

let ws = null;
let myId = null;
let myColor = "#5da9ff";
let myName = "Игрок";
let level = null;
let won = false;

/** @type {Map<string, RemotePlayer>} */
const remotePlayers = new Map();

const me = {
  x: 0, y: 0, vx: 0, vy: 0,
  onGround: false,
  facing: 1,
  atFinish: false,
};

const keys = { left: false, right: false, jump: false };

let hintText = "";
let hintUntil = 0;

joinBtn.addEventListener("click", join);
nameInput.addEventListener("keydown", (e) => { if (e.key === "Enter") join(); });

function join() {
  const name = (nameInput.value || "Игрок").trim().slice(0, 16) || "Игрок";
  myName = name;
  connect();
}

function connect() {
  statusEl.textContent = "Подключение...";
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => { statusEl.textContent = "Подключено. Ждём данные уровня..."; };
  ws.onclose = () => { statusEl.textContent = "Соединение закрыто."; };
  ws.onerror = () => { statusEl.textContent = "Ошибка соединения с сервером."; };
  ws.onmessage = (ev) => handleMessage(JSON.parse(ev.data));
}

function handleMessage(msg) {
  switch (msg.type) {
    case "full":
      statusEl.textContent = "Комната заполнена (максимум 4 игрока). Попробуйте позже.";
      ws.close();
      break;

    case "init":
      myId = msg.id;
      myColor = msg.color;
      level = msg.level;
      me.x = level.start.x;
      me.y = level.start.y;
      for (const p of msg.players || []) {
        if (p.id !== myId) remotePlayers.set(p.id, toRemote(p));
      }
      startGame();
      break;

    case "join":
      if (msg.player.id !== myId) remotePlayers.set(msg.player.id, toRemote(msg.player));
      break;

    case "leave":
      remotePlayers.delete(msg.id);
      break;

    case "move":
      if (msg.id !== myId) {
        const rp = remotePlayers.get(msg.id) || toRemote({ color: "#999" });
        rp.x = msg.x; rp.y = msg.y; rp.facing = msg.facing; rp.name = msg.name || rp.name;
        rp.atFinish = msg.atFinish;
        remotePlayers.set(msg.id, rp);
      }
      break;

    case "hint":
      hintText = msg.text;
      hintUntil = performance.now() + 7000;
      break;

    case "win":
      won = true;
      break;
  }
}

function toRemote(p) {
  return { x: p.x, y: p.y, facing: 1, color: p.color || "#999", name: p.name || "Игрок", atFinish: !!p.atFinish };
}

function startGame() {
  lobby.style.display = "none";
  canvas.style.display = "block";
  hud.style.display = "block";
  requestAnimationFrame(loop);
}

window.addEventListener("keydown", (e) => setKey(e.code, true));
window.addEventListener("keyup", (e) => setKey(e.code, false));

function setKey(code, value) {
  if (code === "ArrowLeft" || code === "KeyA") keys.left = value;
  if (code === "ArrowRight" || code === "KeyD") keys.right = value;
  if (code === "ArrowUp" || code === "KeyW" || code === "Space") keys.jump = value;
}

let lastTime = performance.now();
let lastSend = 0;

function loop(now) {
  const dt = Math.min((now - lastTime) / 1000, 0.05);
  lastTime = now;

  if (!won) update(dt);
  render(now);
  requestAnimationFrame(loop);
}

function update(dt) {
  me.vx = 0;
  if (keys.left) { me.vx = -MOVE_SPEED; me.facing = -1; }
  if (keys.right) { me.vx = MOVE_SPEED; me.facing = 1; }

  if (keys.jump && me.onGround) {
    me.vy = JUMP_VELOCITY;
    me.onGround = false;
  }

  me.vy += GRAVITY * dt;
  me.x += me.vx * dt;
  me.y += me.vy * dt;

  me.onGround = false;
  let fell = false;

  for (const pf of level.platforms) {
    if (resolveCollision(pf)) me.onGround = true;
  }

  if (me.y > level.height + 200) {
    me.x = level.start.x;
    me.y = level.start.y;
    me.vx = 0;
    me.vy = 0;
    fell = true;
  }

  if (me.x < 0) me.x = 0;

  const f = level.finish;
  me.atFinish = aabbOverlap(me.x, me.y, PLAYER_W, PLAYER_H, f.x, f.y - 60, f.w, f.h + 60);

  const now = performance.now();
  if (now - lastSend > SEND_INTERVAL_MS) {
    lastSend = now;
    send({
      type: "move",
      name: myName,
      x: me.x, y: me.y, vx: me.vx, vy: me.vy,
      facing: me.facing,
      atFinish: me.atFinish,
      fell,
    });
  } else if (fell) {
    send({ type: "move", name: myName, x: me.x, y: me.y, vx: me.vx, vy: me.vy, facing: me.facing, atFinish: me.atFinish, fell: true });
  }
}

function resolveCollision(pf) {
  const wasAbove = me.y + PLAYER_H - me.vy * (1 / 60) <= pf.y + 1;
  if (!aabbOverlap(me.x, me.y, PLAYER_W, PLAYER_H, pf.x, pf.y, pf.w, pf.h)) return false;

  if (me.vy >= 0 && wasAbove) {
    me.y = pf.y - PLAYER_H;
    me.vy = 0;
    return true;
  }
  // боковое столкновение — просто останавливаем горизонтальное движение
  if (me.x + PLAYER_W > pf.x && me.x < pf.x) me.x = pf.x - PLAYER_W;
  else if (me.x < pf.x + pf.w && me.x + PLAYER_W > pf.x + pf.w) me.x = pf.x + pf.w;
  return false;
}

function aabbOverlap(ax, ay, aw, ah, bx, by, bw, bh) {
  return ax < bx + bw && ax + aw > bx && ay < by + bh && ay + ah > by;
}

function send(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
}

function render(now) {
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  if (!level) return;

  const camX = clamp(me.x - canvas.width / 2, 0, Math.max(0, level.width - canvas.width));

  ctx.save();
  ctx.translate(-camX, 0);

  // платформы
  ctx.fillStyle = "#4a5578";
  for (const pf of level.platforms) {
    ctx.fillRect(pf.x, pf.y, pf.w, pf.h);
  }

  // финиш
  const f = level.finish;
  ctx.fillStyle = "#5dff8a";
  ctx.fillRect(f.x + f.w / 2 - 3, f.y - 60, 6, 60);
  ctx.beginPath();
  ctx.moveTo(f.x + f.w / 2 + 3, f.y - 60);
  ctx.lineTo(f.x + f.w / 2 + 34, f.y - 50);
  ctx.lineTo(f.x + f.w / 2 + 3, f.y - 40);
  ctx.closePath();
  ctx.fill();

  // другие игроки
  for (const [id, rp] of remotePlayers) {
    drawPlayer(rp.x, rp.y, rp.color, rp.name, rp.atFinish);
  }

  // я
  drawPlayer(me.x, me.y, myColor, myName, me.atFinish, true);

  ctx.restore();

  // HUD
  const total = remotePlayers.size + 1;
  const atFinishCount = [...remotePlayers.values()].filter(p => p.atFinish).length + (me.atFinish ? 1 : 0);
  hud.textContent = `Игроков в комнате: ${total}/4 · на финише: ${atFinishCount}/${total}`;

  if (now < hintUntil && hintText) {
    drawHint(hintText);
  }

  if (won) {
    ctx.fillStyle = "rgba(0,0,0,0.55)";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = "#5dff8a";
    ctx.font = "bold 42px sans-serif";
    ctx.textAlign = "center";
    ctx.fillText("Победа! Команда добралась до финиша 🎉", canvas.width / 2, canvas.height / 2);
    ctx.textAlign = "left";
  }
}

function drawPlayer(x, y, color, name, atFinish, isMe) {
  ctx.fillStyle = color;
  ctx.fillRect(x, y, PLAYER_W, PLAYER_H);
  if (atFinish) {
    ctx.strokeStyle = "#5dff8a";
    ctx.lineWidth = 2;
    ctx.strokeRect(x - 2, y - 2, PLAYER_W + 4, PLAYER_H + 4);
  }
  ctx.fillStyle = "#eef0f5";
  ctx.font = "12px sans-serif";
  ctx.textAlign = "center";
  ctx.fillText(name + (isMe ? " (вы)" : ""), x + PLAYER_W / 2, y - 8);
  ctx.textAlign = "left";
}

function drawHint(text) {
  ctx.save();
  ctx.fillStyle = "rgba(20, 24, 36, 0.85)";
  const pad = 14;
  ctx.font = "15px sans-serif";
  const w = Math.min(canvas.width - 40, ctx.measureText(text).width + pad * 2);
  ctx.fillRect((canvas.width - w) / 2, 14, w, 38);
  ctx.fillStyle = "#ffd25d";
  ctx.textAlign = "center";
  ctx.fillText("🤖 " + text, canvas.width / 2, 14 + 24);
  ctx.textAlign = "left";
  ctx.restore();
}

function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }
