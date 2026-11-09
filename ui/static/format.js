// Numbers and dates as Brazilian readers expect them.

const decimal = (digits) => new Intl.NumberFormat("pt-BR", { maximumFractionDigits: digits, minimumFractionDigits: digits });
const number0 = decimal(0);
const number1 = decimal(1);

export const int = (v) => number0.format(v);
export const one = (v) => number1.format(v);

// short keeps small values readable: 4,2 but 286.
export const short = (v) => (Math.abs(v) < 10 ? one(v) : int(v));

export function bytes(n) {
  if (n >= 1e9) return `${one(n / 1e9)} GB`;
  return `${int(n / 1e6)} MB`;
}

export function hours(ms) {
  const minutes = Math.round(ms / 60000);
  if (minutes < 60) return `${minutes} min`;
  const rest = minutes % 60;
  return `${Math.floor(minutes / 60)}h${rest ? String(rest).padStart(2, "0") : ""}`;
}

export const pad = (v) => String(v).padStart(2, "0");
export const hourLabel = (h) => `${pad(h)}h`;
export const clock = (value) => {
  const d = new Date(value);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

// A day is read at noon so no time zone can move it to a neighboring date.
const dayDate = (day) => new Date(`${day}T12:00:00`);

export const dateLong = (day) => dayDate(day).toLocaleDateString("pt-BR", { weekday: "long", day: "numeric", month: "long" });
export const dateChip = (day) => {
  const d = dayDate(day);
  return { weekday: d.toLocaleDateString("pt-BR", { weekday: "short" }).replace(".", ""), date: `${pad(d.getDate())}/${pad(d.getMonth() + 1)}` };
};

export const qualityWord = { good: "Boa", fair: "Regular", poor: "Ruim", unknown: "Sem leitura" };
export const qualityClass = (q) => (q === "good" || q === "fair" || q === "poor" ? q : "unknown");

export const hourOfDay = (value) => {
  const d = new Date(value);
  return d.getHours() + d.getMinutes() / 60;
};
