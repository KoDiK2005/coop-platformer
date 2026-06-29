// Кооп-платформер: лобби с готовностью, клиентская физика, рендер
// человечков на Canvas, синхронизация по WebSocket.

const canvas = document.getElementById("game");
const ctx = canvas.getContext("2d");
const nameScreen = document.getElementById("nameScreen");
const waitingRoom = document.getElementById("waitingRoom");
const nameInput = document.getElementById("nameInput");
const joinBtn = document.getElementById("joinBtn");
const statusEl = document.getElementById("status");
const hud = document.getElementById("hud");
const playerListEl = document.getElementById("playerList");
const readyBtn = document.getElementById("readyBtn");
const lobbyHintEl = document.getElementById("lobbyHint");
const countdownTextEl = document.getElementById("countdownText");
const spectatorNoticeEl = document.getElementById("spectatorNotice");

const MIN_PLAYERS_TO_START = 2;
const countdownColors = ["#ff5d5d", "#ffd25d", "#5dff8a", "#5da9ff", "#ff5dee"];

const GRAVITY = 1800;
const MOVE_SPEED = 320;
const JUMP_VELOCITY = -650;
const PLAYER_W = 22;
const PLAYER_H = 42;
const SEND_INTERVAL_MS = 50;

let ws = null;
let myId = null;
let myColor = "#5da9ff";
let myName = "Игрок";
let myReady = false;
let level = null;
let won = false;
let spectating = false;
let inGame = false;

/** @type {Map<string, RemotePlayer>} */
const remotePlayers = new Map();

const me = {
  x: 0, y: 0, vx: 0, vy: 0,
  onGround: true,
  facing: 1,
  atFinish: false,
  trail: [],
};

const keys = { left: false, right: false, jump: false };

let hintText = "";
let hintUntil = 0;

const confetti = [];

// ---------- звук (синтез через WebAudio, без файлов) ----------
let audioCtx = null;
function sfx(freqStart, freqEnd, durationMs, type = "sine", volume = 0.15) {
  try {
    audioCtx = audioCtx || new (window.AudioContext || window.webkitAudioContext)();
    const osc = audioCtx.createOscillator();
    const gain = audioCtx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freqStart, audioCtx.currentTime);
    osc.frequency.exponentialRampToValueAtTime(Math.max(freqEnd, 1), audioCtx.currentTime + durationMs / 1000);
    gain.gain.setValueAtTime(volume, audioCtx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audioCtx.currentTime + durationMs / 1000);
    osc.connect(gain).connect(audioCtx.destination);
    osc.start();
    osc.stop(audioCtx.currentTime + durationMs / 1000);
  } catch (e) { /* звук не критичен для игры */ }
}
function playJumpSound() { sfx(420, 720, 140, "square", 0.08); }
function playWinSound() {
  sfx(440, 880, 180, "sine", 0.12);
  setTimeout(() => sfx(660, 1100, 220, "sine", 0.12), 140);
  setTimeout(() => sfx(880, 1320, 300, "sine", 0.12), 300);
}

// ---------- лобби ----------
joinBtn.addEventListener("click", join);
nameInput.addEventListener("keydown", (e) => { if (e.key === "Enter") join(); });
readyBtn.addEventListener("click", toggleReady);

function join() {
  const name = (nameInput.value || "Игрок").trim().slice(0, 16) || "Игрок";
  myName = name;
  connect(name);
}

let intentionalClose = false;

function connect(name) {
  statusEl.textContent = "Подключение...";
  intentionalClose = false;
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/ws?name=${encodeURIComponent(name)}`);

  ws.onopen = () => { statusEl.textContent = ""; };
  ws.onclose = () => {
    if (!intentionalClose) {
      showConnectionLost("Соединение с сервером потеряно. Введите имя и подключитесь снова.");
    }
    intentionalClose = false;
  };
  ws.onerror = () => { statusEl.textContent = "Ошибка соединения с сервером."; };
  ws.onmessage = (ev) => handleMessage(JSON.parse(ev.data));
}

// Без этого разрыв связи (закрыл вкладку у партнёра, упал Wi-Fi, сервер
// перезапустили) оставлял экран зрителя замороженным на последнем известном
// состоянии без какой-либо обратной связи — выглядело как "зависшая" игра.
function showConnectionLost(message) {
  waitingRoom.style.display = "none";
  canvas.style.display = "none";
  canvas.style.filter = "none";
  hud.style.display = "none";
  nameScreen.style.display = "block";
  statusEl.textContent = message;
  level = null;
  won = false;
  inGame = false;
  remotePlayers.clear();
}

function toggleReady() {
  send({ type: "ready" });
}

function handleMessage(msg) {
  switch (msg.type) {
    case "full":
      statusEl.textContent = "Комната заполнена (максимум 4 игрока). Попробуйте позже.";
      intentionalClose = true;
      ws.close();
      break;

    case "lobby":
      myId = msg.yourId;
      spectating = msg.spectating;
      const me_ = msg.players.find(p => p.id === myId);
      if (me_) { myColor = me_.color; myReady = me_.ready; }

      // Сервер шлёт lobby-снимок и зрителям в очереди, и активным игрокам
      // (например, сразу после "start" — чтобы зрители увидели смену
      // состояния). Если мы уже в игре, этот снимок не должен возвращать
      // нас в комнату ожидания — раньше именно так "съедало" старт раунда.
      if (inGame && msg.state !== "waiting") break;
      inGame = false;
      showWaitingRoom(msg);
      break;

    case "countdown":
      if (msg.seconds > 0) showCountdownNumber(msg.seconds);
      else {
        countdownTextEl.textContent = "";
        countdownTextEl.classList.remove("pop");
      }
      break;

    case "start":
      level = msg.level;
      remotePlayers.clear();
      for (const p of msg.players) {
        if (p.id === myId) {
          me.x = p.x; me.y = p.y; me.vx = 0; me.vy = 0; me.onGround = true; me.atFinish = false;
        } else {
          remotePlayers.set(p.id, toRemote(p));
        }
      }
      won = false;
      hintText = "";
      inGame = true;
      startGame();
      break;

    case "move":
      if (msg.id !== myId) {
        const rp = remotePlayers.get(msg.id) || toRemote({ color: "#999" });
        rp.x = msg.x; rp.y = msg.y; rp.vx = msg.vx; rp.vy = msg.vy;
        rp.facing = msg.facing; rp.onGround = msg.onGround;
        rp.name = msg.name || rp.name;
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
      playWinSound();
      spawnConfetti();
      triggerShake(22, 700);
      break;
  }
}

function showCountdownNumber(n) {
  countdownTextEl.textContent = n;
  countdownTextEl.style.color = countdownColors[n % countdownColors.length];
  countdownTextEl.classList.remove("pop");
  void countdownTextEl.offsetWidth; // форсируем перезапуск CSS-анимации
  countdownTextEl.classList.add("pop");
  sfx(260 + n * 70, 200, 160, "triangle", 0.1);
}

function toRemote(p) {
  return {
    x: p.x, y: p.y, vx: 0, vy: 0, onGround: true, facing: 1,
    color: p.color || "#999", name: p.name || "Игрок", atFinish: !!p.atFinish,
    trail: [],
  };
}

function showWaitingRoom(msg) {
  nameScreen.style.display = "none";
  canvas.style.display = "none";
  canvas.style.filter = "none";
  hud.style.display = "none";
  waitingRoom.style.display = "block";

  playerListEl.innerHTML = "";
  for (const p of msg.players) {
    const li = document.createElement("li");
    const dot = document.createElement("span");
    dot.className = "dot";
    dot.style.background = p.color;
    const label = document.createElement("span");
    label.textContent = p.name + (p.id === myId ? " (вы)" : "");
    const tag = document.createElement("span");
    tag.className = "readyTag" + (p.ready ? " on" : "");
    tag.textContent = p.ready ? "Готов" : "Не готов";
    li.append(dot, label, tag);
    playerListEl.appendChild(li);
  }

  if (msg.queueCount > 0) {
    const li = document.createElement("li");
    li.style.color = "#6c7388";
    li.textContent = `Зрителей в очереди: ${msg.queueCount} (присоединятся к следующему раунду)`;
    playerListEl.appendChild(li);
  }

  readyBtn.classList.toggle("ready-on", myReady);
  readyBtn.textContent = myReady ? "Готов ✓" : "Готов";
  readyBtn.style.display = spectating ? "none" : "inline-block";
  spectatorNoticeEl.style.display = spectating ? "block" : "none";

  lobbyHintEl.textContent = spectating ? "" : computeLobbyHint(msg.players);
}

function computeLobbyHint(players) {
  if (players.length < MIN_PLAYERS_TO_START) {
    return `Нужно минимум ${MIN_PLAYERS_TO_START} игрока, чтобы начать (сейчас ${players.length})`;
  }
  const readyCount = players.filter(p => p.ready).length;
  if (readyCount < players.length) {
    return `Готовы ${readyCount}/${players.length} — нажмите «Готов»`;
  }
  return "Все готовы — старт!";
}

// ---------- управление ----------
window.addEventListener("keydown", (e) => setKey(e.code, true));
window.addEventListener("keyup", (e) => setKey(e.code, false));

function setKey(code, value) {
  if (code === "ArrowLeft" || code === "KeyA") keys.left = value;
  if (code === "ArrowRight" || code === "KeyD") keys.right = value;
  if (code === "ArrowUp" || code === "KeyW" || code === "Space") keys.jump = value;
}

// ---------- игровой цикл ----------
function startGame() {
  waitingRoom.style.display = "none";
  canvas.style.display = "block";
  hud.style.display = "block";
  requestAnimationFrame(loop);
}

let lastTime = performance.now();
let lastSend = 0;
let walkPhase = 0;

function loop(now) {
  const dt = Math.min((now - lastTime) / 1000, 0.05);
  lastTime = now;

  if (!won && canvas.style.display !== "none") update(dt);
  if (won) updateConfetti(dt);
  render(now);

  if (canvas.style.display !== "none") requestAnimationFrame(loop);
}

function update(dt) {
  me.vx = 0;
  if (keys.left) { me.vx = -MOVE_SPEED; me.facing = -1; }
  if (keys.right) { me.vx = MOVE_SPEED; me.facing = 1; }

  if (keys.jump && me.onGround) {
    me.vy = JUMP_VELOCITY;
    me.onGround = false;
    playJumpSound();
  }

  me.vy += GRAVITY * dt;
  me.x += me.vx * dt;
  me.y += me.vy * dt;

  if (me.vx !== 0) walkPhase += dt * 10;

  const wasOnGround = me.onGround;
  const fallSpeed = me.vy;
  me.onGround = false;
  let fell = false;

  for (const pf of level.platforms) {
    if (resolveCollision(pf)) me.onGround = true;
  }

  if (!wasOnGround && me.onGround && fallSpeed > 900) {
    triggerShake(6, 150);
  }

  if (me.y > level.height + 200) {
    me.x = level.start.x;
    me.y = level.start.y;
    me.vx = 0;
    me.vy = 0;
    fell = true;
    triggerShake(10, 200);
  }

  if (me.x < 0) me.x = 0;

  const f = level.finish;
  me.atFinish = aabbOverlap(me.x, me.y, PLAYER_W, PLAYER_H, f.x, f.y - 60, f.w, f.h + 60);

  const now = performance.now();
  if (now - lastSend > SEND_INTERVAL_MS || fell) {
    lastSend = now;
    send({
      type: "move",
      x: me.x, y: me.y, vx: me.vx, vy: me.vy,
      facing: me.facing,
      onGround: me.onGround,
      atFinish: me.atFinish,
      fell,
    });
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

// ---------- рендер (максимально психоделический) ----------
let shakeUntil = 0;
let shakeMag = 0;

function triggerShake(magnitude, durationMs) {
  shakeMag = magnitude;
  shakeUntil = performance.now() + durationMs;
}

function render(now) {
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  if (!level) return;

  // непрерывный сдвиг оттенка всей картинки — главный "наркоманский" эффект
  const hueDeg = (now / 18) % 360;
  canvas.style.filter = `hue-rotate(${hueDeg}deg) saturate(1.6) contrast(1.08)`;

  const camX = clamp(me.x - canvas.width / 2, 0, Math.max(0, level.width - canvas.width));
  const camY = clamp(me.y - canvas.height / 2, 0, Math.max(0, level.height - canvas.height));

  let shakeX = 0, shakeY = 0;
  if (now < shakeUntil) {
    shakeX = (Math.random() - 0.5) * shakeMag;
    shakeY = (Math.random() - 0.5) * shakeMag;
  }

  drawPsychedelicBackdrop(now, camX);

  ctx.save();
  ctx.translate(-camX + shakeX, -camY + shakeY);

  for (const pf of level.platforms) {
    const hue = (now / 9 + pf.x * 0.06) % 360;
    ctx.save();
    ctx.shadowColor = `hsl(${hue}, 100%, 60%)`;
    ctx.shadowBlur = 16;
    ctx.fillStyle = `hsl(${hue}, 75%, 38%)`;
    ctx.fillRect(pf.x, pf.y, pf.w, pf.h);
    ctx.fillStyle = `hsl(${hue}, 100%, 72%)`;
    ctx.fillRect(pf.x, pf.y, pf.w, 4);
    ctx.restore();
  }

  drawRainbowFinish(level.finish, now);

  pushTrail(me.trail, me.x, me.y, now);
  for (const [, rp] of remotePlayers) pushTrail(rp.trail, rp.x, rp.y, now);

  for (const [, rp] of remotePlayers) drawTrail(rp.trail, now);
  drawTrail(me.trail, now);

  for (const [, rp] of remotePlayers) {
    drawHuman(rp.x, rp.y, rp.color, rp.name, rp.atFinish, rp.facing, rp.vx !== 0, rp.onGround, false);
  }
  drawHuman(me.x, me.y, myColor, myName, me.atFinish, me.facing, me.vx !== 0, me.onGround, true);

  ctx.restore();

  drawConfetti();

  const total = remotePlayers.size + 1;
  const atFinishCount = [...remotePlayers.values()].filter(p => p.atFinish).length + (me.atFinish ? 1 : 0);
  hud.textContent = `Игроков в комнате: ${total}/4 · на финише: ${atFinishCount}/${total}`;

  if (now < hintUntil && hintText) drawHint(hintText);

  if (won) {
    ctx.fillStyle = "rgba(0,0,0,0.45)";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.font = "bold 36px sans-serif";
    ctx.textAlign = "center";
    ctx.fillStyle = `hsl(${(now / 4) % 360}, 100%, 65%)`;
    ctx.fillText("Победа! Команда добралась до финиша 🎉", canvas.width / 2, canvas.height / 2 - 10);
    ctx.font = "16px sans-serif";
    ctx.fillStyle = "#eef0f5";
    ctx.fillText("Возвращение в комнату ожидания...", canvas.width / 2, canvas.height / 2 + 26);
    ctx.textAlign = "left";
  }
}

// фон: цикличное по оттенку небо + плавающие неоновые кольца + параллакс-холмы
function drawPsychedelicBackdrop(now, camX) {
  const h1 = (now / 25) % 360;
  const h2 = (h1 + 110) % 360;
  const grad = ctx.createLinearGradient(0, 0, 0, canvas.height);
  grad.addColorStop(0, `hsl(${h1}, 70%, 24%)`);
  grad.addColorStop(1, `hsl(${h2}, 70%, 10%)`);
  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  ctx.save();
  ctx.globalCompositeOperation = "lighter";
  for (let i = 0; i < 6; i++) {
    const t = now / 1000 + i * 1.7;
    const r = 50 + (Math.sin(t * 0.6) * 0.5 + 0.5) * 230;
    const cx = canvas.width * 0.5 + Math.sin(t * 0.31 + i) * canvas.width * 0.4;
    const cy = canvas.height * 0.35 + Math.cos(t * 0.23 + i) * canvas.height * 0.3;
    const hue = (now / 9 + i * 60) % 360;
    ctx.beginPath();
    ctx.fillStyle = `hsla(${hue}, 100%, 60%, 0.07)`;
    ctx.arc(cx, cy, r, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.restore();

  ctx.save();
  const cloudOffset = -(camX * 0.2) % 300;
  for (let i = -1; i < canvas.width / 300 + 2; i++) {
    const x = cloudOffset + i * 300;
    const hue = (now / 14 + i * 40) % 360;
    ctx.fillStyle = `hsla(${hue}, 90%, 75%, 0.10)`;
    drawCloud(x + 40, 70);
    drawCloud(x + 180, 130);
  }

  const hillOffset = -(camX * 0.45) % 400;
  for (let i = -1; i < canvas.width / 400 + 2; i++) {
    const x = hillOffset + i * 400;
    const hue = (now / 11 + i * 50) % 360;
    ctx.fillStyle = `hsla(${hue}, 70%, 45%, 0.45)`;
    ctx.beginPath();
    ctx.ellipse(x + 100, canvas.height - 20, 160, 70, 0, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.restore();
}

function drawCloud(x, y) {
  ctx.beginPath();
  ctx.ellipse(x, y, 30, 16, 0, 0, Math.PI * 2);
  ctx.ellipse(x + 24, y + 4, 22, 13, 0, 0, Math.PI * 2);
  ctx.ellipse(x - 24, y + 4, 22, 13, 0, 0, Math.PI * 2);
  ctx.fill();
}

function drawRainbowFinish(f, now) {
  ctx.save();
  ctx.fillStyle = "#bfae7a";
  ctx.fillRect(f.x + f.w / 2 - 3, f.y - 60, 6, 60);

  const stripes = 6;
  const stripeH = 60 / stripes;
  for (let i = 0; i < stripes; i++) {
    const hue = (now / 4 + i * 55) % 360;
    ctx.fillStyle = `hsl(${hue}, 100%, 60%)`;
    ctx.beginPath();
    ctx.moveTo(f.x + f.w / 2 + 3, f.y - 60 + i * stripeH);
    ctx.lineTo(f.x + f.w / 2 + 34, f.y - 60 + i * stripeH + stripeH / 2);
    ctx.lineTo(f.x + f.w / 2 + 3, f.y - 60 + (i + 1) * stripeH);
    ctx.closePath();
    ctx.fill();
  }
  ctx.restore();
}

// ---------- радужный шлейф за игроками ----------
function pushTrail(trail, x, y, now) {
  trail.push({ x: x + PLAYER_W / 2, y: y + PLAYER_H / 2, t: now });
  while (trail.length > 16) trail.shift();
}

function drawTrail(trail, now) {
  for (let i = 0; i < trail.length; i++) {
    const pt = trail[i];
    const progress = i / trail.length;
    const hue = (now / 5 + i * 22) % 360;
    ctx.beginPath();
    ctx.fillStyle = `hsla(${hue}, 100%, 65%, ${progress * 0.45})`;
    ctx.arc(pt.x, pt.y, 3 + progress * 7, 0, Math.PI * 2);
    ctx.fill();
  }
}

// человечек: голова + туловище + руки/ноги, с простой анимацией ходьбы/прыжка
function drawHuman(x, y, shirtColor, name, atFinish, facing, walking, onGround, isMe) {
  const cx = x + PLAYER_W / 2;
  const skin = "#f2c79c";
  const legSwing = onGround && walking ? Math.sin(walkPhase) * 10 : 0;

  ctx.save();
  ctx.translate(cx, y);
  if (facing < 0) ctx.scale(-1, 1);

  // ноги
  ctx.strokeStyle = "#33384a";
  ctx.lineWidth = 5;
  ctx.lineCap = "round";
  if (onGround) {
    ctx.beginPath();
    ctx.moveTo(-5, 26); ctx.lineTo(-5 + legSwing * 0.3, 41);
    ctx.moveTo(5, 26); ctx.lineTo(5 - legSwing * 0.3, 41);
    ctx.stroke();
  } else {
    ctx.beginPath();
    ctx.moveTo(-5, 26); ctx.lineTo(-9, 38);
    ctx.moveTo(5, 26); ctx.lineTo(9, 38);
    ctx.stroke();
  }

  // туловище
  ctx.fillStyle = shirtColor;
  ctx.beginPath();
  ctx.roundRect ? ctx.roundRect(-9, 4, 18, 24, 4) : ctx.rect(-9, 4, 18, 24);
  ctx.fill();

  // руки
  ctx.strokeStyle = shirtColor;
  ctx.lineWidth = 5;
  if (onGround) {
    ctx.beginPath();
    ctx.moveTo(-8, 8); ctx.lineTo(-8 - legSwing * 0.3, 22);
    ctx.moveTo(8, 8); ctx.lineTo(8 + legSwing * 0.3, 22);
    ctx.stroke();
  } else {
    ctx.beginPath();
    ctx.moveTo(-8, 8); ctx.lineTo(-14, -2);
    ctx.moveTo(8, 8); ctx.lineTo(14, -2);
    ctx.stroke();
  }

  // голова
  ctx.fillStyle = skin;
  ctx.beginPath();
  ctx.arc(0, -8, 10, 0, Math.PI * 2);
  ctx.fill();

  // глаза (смотрят по направлению взгляда)
  ctx.fillStyle = "#1b1f2a";
  ctx.beginPath();
  ctx.arc(4, -9, 1.6, 0, Math.PI * 2);
  ctx.fill();

  ctx.restore();

  if (atFinish) {
    ctx.strokeStyle = "#5dff8a";
    ctx.lineWidth = 2;
    ctx.strokeRect(x - 4, y - 22, PLAYER_W + 8, PLAYER_H + 26);
  }

  ctx.fillStyle = "#eef0f5";
  ctx.font = "12px sans-serif";
  ctx.textAlign = "center";
  ctx.fillText(name + (isMe ? " (вы)" : ""), cx, y - 24);
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

// ---------- конфетти на победу ----------
function spawnConfetti() {
  confetti.length = 0;
  for (let i = 0; i < 160; i++) {
    confetti.push({
      x: Math.random() * canvas.width,
      y: -20 - Math.random() * canvas.height * 0.5,
      vx: (Math.random() - 0.5) * 100,
      vy: 120 + Math.random() * 220,
      size: 4 + Math.random() * 6,
      hue: Math.random() * 360,
      rot: Math.random() * Math.PI,
      vrot: (Math.random() - 0.5) * 8,
    });
  }
}

function updateConfetti(dt) {
  for (const c of confetti) {
    c.x += c.vx * dt;
    c.y += c.vy * dt;
    c.rot += c.vrot * dt;
  }
}

function drawConfetti() {
  const now = performance.now();
  for (const c of confetti) {
    if (c.y > canvas.height + 20) continue;
    ctx.save();
    ctx.translate(c.x, c.y);
    ctx.rotate(c.rot);
    const hue = (c.hue + now / 10) % 360;
    ctx.fillStyle = `hsl(${hue}, 100%, 65%)`;
    ctx.shadowColor = `hsl(${hue}, 100%, 70%)`;
    ctx.shadowBlur = 8;
    ctx.fillRect(-c.size / 2, -c.size / 2, c.size, c.size);
    ctx.restore();
  }
}

function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }
