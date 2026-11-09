// The panel's drawings, in plain SVG. Hours without measurements stay hatched
// and lines are interrupted there: nothing is invented to fill a gap.

import { s } from "./dom.js";
import { clock as clockTime, hourLabel, hourOfDay, int, qualityWord, short } from "./format.js";

const color = { good: "var(--good)", fair: "var(--fair)", poor: "var(--poor)" };
const wash = { good: "var(--good-wash)", fair: "var(--fair-wash)", poor: "var(--poor-wash)" };

function hatch(id) {
  return s("defs", null, s("pattern", { id, width: 6, height: 6, patternUnits: "userSpaceOnUse", patternTransform: "rotate(45)" },
    s("line", { x1: 0, y1: 0, x2: 0, y2: 6, stroke: "var(--none)", "stroke-width": 2 })));
}

function hourTitle(index, hour) {
  const name = hourLabel(index);
  if (!hour || !hour.rounds) return `${name}: sem leitura`;
  return `${name}: ${qualityWord[hour.grade].toLowerCase()}, ping ${int(hour.latency_ms)} ms`;
}

// clock24 draws the day as a ring of 24 slices, with midnight at the top.
export function clock24({ hours, title, subtitle, nowHour }) {
  const c = 160, r0 = 100, r1 = 142, gap = 1.2;
  const point = (r, deg) => {
    const rad = (deg - 90) * Math.PI / 180;
    return [c + r * Math.cos(rad), c + r * Math.sin(rad)];
  };
  const slice = (a0, a1) => {
    const [x0, y0] = point(r1, a0), [x1, y1] = point(r1, a1);
    const [x2, y2] = point(r0, a1), [x3, y3] = point(r0, a0);
    return `M${x0} ${y0}A${r1} ${r1} 0 0 1 ${x1} ${y1}L${x2} ${y2}A${r0} ${r0} 0 0 0 ${x3} ${y3}Z`;
  };

  const svg = s("svg", { viewBox: "0 0 320 320", role: "img", "aria-label": "Qualidade da internet hora a hora, em um relógio de 24 horas" }, hatch("clock-hatch"));
  for (let h = 0; h < 24; h++) {
    const hour = hours?.[h];
    svg.append(s("path", {
      class: "seg", d: slice(h * 15 + gap / 2, (h + 1) * 15 - gap / 2),
      fill: hour?.rounds && color[hour.grade] ? color[hour.grade] : "url(#clock-hatch)",
    }, s("title", null, hourTitle(h, hour))));
  }
  for (let h = 0; h < 24; h += 6) {
    const [x, y] = point(r1 + 14, h * 15);
    svg.append(s("text", { class: "hour-label", x, y, "text-anchor": "middle", "dominant-baseline": "middle" }, hourLabel(h)));
  }
  for (let h = 0; h < 24; h++) {
    const [x0, y0] = point(r0 - 6, h * 15), [x1, y1] = point(r0 - (h % 6 === 0 ? 14 : 9), h * 15);
    svg.append(s("line", { x1: x0, y1: y0, x2: x1, y2: y1, stroke: "var(--rule)", "stroke-width": h % 6 === 0 ? 2 : 1 }));
  }
  if (nowHour !== undefined) {
    const [x0, y0] = point(r0 - 4, nowHour * 15), [x1, y1] = point(r1 + 5, nowHour * 15);
    svg.append(s("line", { x1: x0, y1: y0, x2: x1, y2: y1, stroke: "var(--ink)", "stroke-width": 2.5, "stroke-linecap": "round" }));
  }
  svg.append(
    s("text", { class: "center-sub", x: c, y: c - 22, "text-anchor": "middle" }, nowHour === undefined ? "nota do dia" : "agora"),
    s("text", { class: "center-title", x: c, y: c + 14, "text-anchor": "middle" }, title),
    s("text", { class: "center-sub", x: c, y: c + 38, "text-anchor": "middle" }, subtitle),
  );
  return svg;
}

const AXIS_TICKS = 4;

// ceiling picks a top for an axis whose ticks all land on round numbers.
function ceiling(value) {
  if (value <= 0) return AXIS_TICKS;
  const tick = value / AXIS_TICKS;
  const magnitude = 10 ** Math.floor(Math.log10(tick));
  const step = [1, 2, 2.5, 3, 4, 5, 10].find((candidate) => tick <= candidate * magnitude) ?? 10;
  return AXIS_TICKS * step * magnitude;
}

function frame({ id, label, height, maxY, hours, unit }) {
  const W = 760, H = height, left = 44, right = 12, top = 18, bottom = 30;
  const iw = W - left - right, ih = H - top - bottom;
  const x = (hour) => left + (hour / 24) * iw;
  const y = (v) => top + ih - (v / maxY) * ih;

  const svg = s("svg", { class: "chart", viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": label }, hatch(id));
  hours.forEach((hour, h) => {
    const graded = hour.rounds > 0 && wash[hour.grade];
    svg.append(s("rect", {
      x: x(h), y: top, width: iw / 24, height: ih,
      fill: graded || `url(#${id})`, opacity: graded ? 1 : 0.35,
    }, s("title", null, hourTitle(h, hour))));
  });
  for (let i = 0; i <= AXIS_TICKS; i++) {
    const v = (maxY / AXIS_TICKS) * i;
    svg.append(
      s("line", { class: "grid", x1: left, x2: W - right, y1: y(v), y2: y(v) }),
      s("text", { class: "axis", x: left - 8, y: y(v), "text-anchor": "end", "dominant-baseline": "middle" }, Number.isInteger(v) ? int(v) : short(v)),
    );
  }
  for (let h = 0; h <= 24; h += 3) {
    svg.append(s("text", { class: "axis", x: x(h), y: H - 8, "text-anchor": "middle" }, hourLabel(h)));
  }
  svg.append(s("text", { class: "axis", x: left - 8, y: 8, "text-anchor": "end" }, unit));
  return { svg, x, y, W, right };
}

// segments splits a series wherever an hour between two points has no data.
function segments(points, hours) {
  const result = [];
  let current = [];
  for (const point of points) {
    const previous = current.at(-1);
    if (previous) {
      for (let h = Math.floor(previous.hour) + 1; h < Math.floor(point.hour); h++) {
        if (!hours[h].rounds) {
          result.push(current);
          current = [];
          break;
        }
      }
    }
    current.push(point);
  }
  if (current.length) result.push(current);
  return result;
}

const path = (points, x, y) => points.map((p, i) => `${i ? "L" : "M"}${x(p.hour).toFixed(1)} ${y(p.value).toFixed(1)}`).join("");

function drawSeries(svg, series, hours, x, y, attrs) {
  for (const part of segments(series, hours)) {
    if (part.length > 1) svg.append(s("path", { d: path(part, x, y), fill: "none", "stroke-linejoin": "round", ...attrs }));
  }
}

const PLOT_LABEL_FLOOR = 170;

function annotate(svg, x, y, label, below) {
  const px = x(hourOfDay(label.at)), py = y(label.value);
  const text = `${label.name} ${short(label.value)} · ${clockTime(label.at)}`;
  const flip = px > 600;
  const tx = flip ? px - 10 - text.length * 7.2 : px + 10;
  // Near the bottom a label below the point would cover the upload line.
  const ty = below && py < PLOT_LABEL_FLOOR ? py + 22 : Math.max(py - 14, 30);
  svg.append(
    s("circle", { cx: px, cy: py, r: 5, fill: "var(--surface)", stroke: "var(--ink)", "stroke-width": 2 }),
    s("rect", { class: "note-bg", x: tx - 4, y: ty - 13, width: text.length * 7.2 + 8, height: 18, rx: 4 }),
    s("text", { class: "note", x: tx, y: ty }, text),
  );
}

// speedChart draws the speed tests over the day's quality bands.
export function speedChart(report, planDownload) {
  const points = report.speed ?? [];
  const highest = Math.max(planDownload || 0, ...points.map((p) => p.download_mbps), ...points.map((p) => p.upload_mbps));
  const maxY = ceiling(highest * 1.08);
  const { svg, x, y, W, right } = frame({ id: "speed-hatch", label: "Download e upload ao longo do dia", height: 260, maxY, hours: report.hours, unit: "Mbps" });

  if (planDownload > 0) {
    svg.append(
      s("line", { class: "plan", x1: x(0), x2: W - right, y1: y(planDownload), y2: y(planDownload) }),
      s("text", { class: "axis", x: W - right, y: y(planDownload) - 6, "text-anchor": "end" }, `seu plano: ${int(planDownload)} Mbps`),
    );
  }

  const down = points.map((p) => ({ hour: hourOfDay(p.at), value: p.download_mbps, local: p.local_traffic }));
  const up = points.map((p) => ({ hour: hourOfDay(p.at), value: p.upload_mbps, local: p.local_traffic }));
  drawSeries(svg, down, report.hours, x, y, { stroke: "var(--ink)", "stroke-width": 2.5 });
  drawSeries(svg, up, report.hours, x, y, { stroke: "var(--ink-soft)", "stroke-width": 2, "stroke-dasharray": "5 4" });

  for (const [series, radius] of [[down, 3.5], [up, 2.5]]) {
    for (const p of series) {
      svg.append(s("circle", {
        cx: x(p.hour), cy: y(p.value), r: radius, "stroke-width": 1.5, stroke: "var(--ink)",
        fill: p.local ? "var(--surface)" : "var(--ink)",
      }, s("title", null, `${short(p.value)} Mbps${p.local ? " (outros programas usavam a rede)" : ""}`)));
    }
  }

  const download = report.download_mbps;
  if (download.count >= 3) {
    annotate(svg, x, y, { name: "máx.", value: download.max, at: download.max_at }, false);
    annotate(svg, x, y, { name: "mín.", value: download.min, at: download.min_at }, true);
  }
  return svg;
}

// latencyChart draws the hourly response time, to the internet and the router.
export function latencyChart(report) {
  const measured = (field) => report.hours.flatMap((hour, h) => (hour.rounds && hour[field] > 0 ? [{ hour: h + 0.5, value: hour[field] }] : []));
  const internet = measured("latency_ms"), gateway = measured("gateway_ms");
  const maxY = ceiling(Math.max(...internet.map((p) => p.value), ...gateway.map((p) => p.value), 1) * 1.1);
  const { svg, x, y } = frame({ id: "latency-hatch", label: "Tempo de resposta por hora", height: 200, maxY, hours: report.hours, unit: "ms" });

  // Hour midpoints: consecutive measured hours connect, a missing one breaks the line.
  const split = (series) => {
    const parts = [];
    for (const point of series) {
      const last = parts.at(-1);
      if (last && point.hour - last.at(-1).hour === 1) last.push(point);
      else parts.push([point]);
    }
    return parts;
  };
  for (const [series, attrs] of [
    [internet, { stroke: "var(--ink)", "stroke-width": 2.5 }],
    [gateway, { stroke: "var(--ink-soft)", "stroke-width": 2, "stroke-dasharray": "5 4" }],
  ]) {
    for (const part of split(series)) {
      if (part.length > 1) svg.append(s("path", { d: path(part, x, y), fill: "none", "stroke-linejoin": "round", ...attrs }));
      else svg.append(s("circle", { cx: x(part[0].hour), cy: y(part[0].value), r: 3, fill: attrs.stroke }));
    }
  }
  return svg;
}
