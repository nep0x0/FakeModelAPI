"use strict";

// ---- Jam digital ----
function updateClock() {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, "0");
  const el = document.getElementById("clock");
  if (el) {
    el.textContent = `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
  }
}

setInterval(updateClock, 1000);
updateClock();

// ---- Penghitung ----
let count = 0;
function tambah(delta) {
  count = delta === 0 ? 0 : count + delta;
  const el = document.getElementById("count");
  if (el) el.textContent = count;
}

// ---- Sapa ----
function greet() {
  const el = document.getElementById("greet-output");
  const now = new Date().getHours();
  const salam = now < 12 ? "Selamat pagi" : now < 18 ? "Selamat siang" : "Selamat malam";
  if (el) el.textContent = `${salam}! Terima kasih sudah mengunjungi halaman ini.`;
}

// ---- Catatan ----
function tambahTodo() {
  const input = document.getElementById("todo-input");
  const list = document.getElementById("todo-list");
  const text = input.value.trim();
  if (!text || !list) return;

  const li = document.createElement("li");
  const span = document.createElement("span");
  span.textContent = text;
  span.style.cursor = "pointer";
  span.onclick = () => li.classList.toggle("done");

  const del = document.createElement("button");
  del.textContent = "hapus";
  del.onclick = () => li.remove();

  li.append(span, del);
  list.appendChild(li);
  input.value = "";
  input.focus();
}

// ---- Tahun footer ----
const yearEl = document.getElementById("year");
if (yearEl) yearEl.textContent = String(new Date().getFullYear());
