// "Relatórios": the daily bulletin.

import { ApiError, api } from "./api.js";
import { clock24, latencyChart, speedChart } from "./charts.js";
import { glyph, h } from "./dom.js";
import { bytes, clock, dateChip, dateLong, hourLabel, hours, int, one, qualityClass, qualityWord, short } from "./format.js";
import { patternsSection, planSection } from "./insights.js";
import { badge, chain, clockLegend, sectionTitle, usageBar } from "./parts.js";

const GLYPHS = {
  unknown: ["M8 20h24"],
  good: ["M20 13a7 7 0 1 0 0 14 7 7 0 0 0 0-14z", "M20 5v4", "M20 31v4", "M5 20h4", "M31 20h4", "M9.4 9.4l2.8 2.8", "M27.8 27.8l2.8 2.8", "M9.4 30.6l2.8-2.8", "M27.8 12.2l2.8-2.8"],
  fair: ["M15 9a5 5 0 1 0 0 10 5 5 0 0 0 0-10z", "M15 4v2", "M5 14h2", "M7.9 6.9l1.4 1.4", "M22.1 6.9l-1.4 1.4", "M13 32h16a6 6 0 0 0 0-12 8 8 0 0 0-15.4 2.6A4.7 4.7 0 0 0 13 32z"],
  poor: ["M11 25h18a6 6 0 0 0 0-12 8 8 0 0 0-15.4 2.6A4.7 4.7 0 0 0 11 25z", "M21 27l-3 5h5l-3 5"],
};

// headline puts the last word in italics on days that were not good.
function headlineNode(text, grade) {
  const clean = text.replace(/\.$/, "");
  const cut = clean.lastIndexOf(" ");
  if (grade === "good" || grade === "unknown" || cut < 0) return h("h1", null, text);
  return h("h1", null, clean.slice(0, cut + 1), h("em", { class: grade }, clean.slice(cut + 1)), ".");
}

function hero(report) {
  const { coverage } = report;
  return h("section", { class: "hero" },
    h("div", null,
      h("p", { class: "kicker" }, report.in_progress ? "Hoje, até agora" : "O dia, em uma frase"),
      headlineNode(report.headline, report.grade),
      h("p", { class: "lede" }, report.lede),
      h("div", { class: "verdict" }, badge(report.grade), h("span", null, `pelo perfil ${report.profile_name}`)),
      h("p", { class: "coverage" },
        `Monitorado ${hours(coverage.monitored_ms)} de ${hours(coverage.window_ms)} · `,
        `${int(coverage.rounds)} medições · ${int(coverage.speed_tests)} testes de velocidade`)),
    h("figure", { class: "clock" },
      clock24({ hours: report.hours, title: qualityWord[qualityClass(report.grade)], subtitle: `${hours(coverage.monitored_ms)} monitoradas` }),
      clockLegend()));
}

function period(p) {
  const q = qualityClass(p.grade);
  const head = h("header", null,
    h("div", null, h("h3", null, p.name), h("div", { class: "hours" }, `${hourLabel(p.from_hour)} – ${hourLabel(p.to_hour)}`)),
    glyph("glyph", "0 0 40 40", ...GLYPHS[q]));
  if (q === "unknown") {
    return h("article", { class: "period", "data-q": q }, head, h("p", { class: "cond" }, p.condition),
      h("p", { class: "why" }, "O AvaNet não mediu neste período. Nenhum dado foi inventado."));
  }
  return h("article", { class: "period", "data-q": q }, head, h("p", { class: "cond" }, p.condition),
    h("dl", null,
      p.download_mbps > 0 && [h("dt", null, "Velocidade"), h("dd", null, `${int(p.download_mbps)} / ${int(p.upload_mbps)} Mbps`)],
      h("dt", null, "Ping"), h("dd", null, `${int(p.latency_ms)} ms`),
      p.outages > 0 && [h("dt", null, "Quedas"), h("dd", null, int(p.outages))]));
}

function speedSection(report, plan) {
  const hasPoints = report.speed?.length > 0;
  return [
    sectionTitle("Velocidade ao longo do dia", h("div", { class: "chart-legend" }, h("span", null, "Download"), h("span", { class: "dashed" }, "Upload"))),
    h("div", { class: "card" },
      hasPoints ? speedChart(report, plan) : h("p", { class: "empty-chart" }, "Nenhum teste de velocidade neste dia."),
      report.ignored_speed_tests > 0 && h("p", { class: "note-line" },
        `${report.ignored_speed_tests === 1 ? "O ponto vazado foi medido" : "Os pontos vazados foram medidos"} enquanto outros programas usavam a rede e ficam fora das notas.`)),
  ];
}

function latencySection(report) {
  return [
    sectionTitle("Tempo de resposta", h("div", { class: "chart-legend" }, h("span", null, "Internet"), h("span", { class: "dashed" }, "Roteador"))),
    h("div", { class: "card" },
      latencyChart(report),
      h("details", { class: "hourly" }, h("summary", null, "Ver hora a hora em tabela"), hourTable(report))),
  ];
}

function hourTable(report) {
  const cell = (hour, value) => (hour.rounds ? value : "—");
  return h("div", { class: "table-wrap", tabindex: 0 }, h("table", null,
    h("thead", null, h("tr", null, ["Hora", "Nota", "Ping (ms)", "Roteador (ms)", "Jitter (ms)", "Perda (%)", "Download", "Upload"].map((t) => h("th", { scope: "col" }, t)))),
    h("tbody", null, report.hours.map((hour, i) => h("tr", null,
      h("td", null, hourLabel(i)),
      h("td", null, hour.rounds ? qualityWord[qualityClass(hour.grade)] : "Sem leitura"),
      h("td", null, cell(hour, int(hour.latency_ms))),
      h("td", null, cell(hour, short(hour.gateway_ms))),
      h("td", null, cell(hour, short(hour.jitter_ms))),
      h("td", null, cell(hour, one(hour.loss_percent))),
      h("td", null, hour.download_mbps > 0 ? int(hour.download_mbps) : "—"),
      h("td", null, hour.upload_mbps > 0 ? int(hour.upload_mbps) : "—"))))));
}

const VERDICT_LINKS = {
  none: ["good", "good"], local: ["poor", "good"], provider: ["good", "poor"], capacity: ["good", "fair"], unknown: ["unknown", "unknown"],
};

const rangeText = ({ from, to }) => `das ${from}h às ${to}h`;

function diagnosis(report) {
  const d = report.diagnosis;
  const [near, far] = VERDICT_LINKS[d.verdict] ?? VERDICT_LINKS.unknown;
  return [
    sectionTitle("Onde estava o problema?", h("p", null, "Comparando o caminho até o roteador com o caminho até a internet")),
    h("section", { class: "split" },
      h("article", { class: "card diagnosis" },
        h("h3", null, d.title),
        d.signal_percent > 0 && h("p", { class: "sub" }, `Sinal do Wi-Fi: ${int(d.signal_percent)}%`),
        chain({
          near: { quality: near, label: d.gateway_ms > 0 ? `Rede · ${short(d.gateway_ms)} ms` : "Rede" },
          far: { quality: far, label: d.internet_ms > 0 ? `Operadora · ${int(d.internet_ms)} ms` : "Operadora" },
        }),
        h("p", null, d.detail)),
      h("article", { class: "card forecast" },
        h("h3", null, "Faixas do dia"),
        h("p", { class: "sub" }, "As horas consecutivas que se destacaram"),
        h("dl", null,
          h("div", null, h("dt", null, "Melhor faixa"), h("dd", null, report.best?.length ? report.best.map(rangeText).join(", ") : "Nenhuma faixa boa")),
          h("div", null, h("dt", null, "Pior faixa"), h("dd", null, report.worst?.length ? report.worst.map(rangeText).join(", ") : "Nenhuma faixa ruim"))))),
  ];
}

function stat(label, unit, s) {
  if (!s.count) return h("div", { class: "stat" }, h("div", { class: "label" }, label), h("div", { class: "value" }, "—"), h("div", { class: "range" }, "sem medições"));
  return h("div", { class: "stat" },
    h("div", { class: "label" }, label),
    h("div", { class: "value" }, short(s.median), h("small", null, unit)),
    h("div", { class: "range" },
      "80% entre ", h("b", null, short(s.p10)), " e ", h("b", null, short(s.p90)), h("br"),
      "máx. ", h("b", null, short(s.max)), ` às ${clock(s.max_at)} · mín. `, h("b", null, short(s.min)), ` às ${clock(s.min_at)}`));
}

function numbers(report) {
  const outages = report.outages ?? [];
  return [
    sectionTitle("Os números do dia", h("p", null, "Mediana em destaque; abaixo, a faixa onde ficaram 80% das medições")),
    h("section", { class: "stats" },
      stat("Download", "Mbps", report.download_mbps),
      stat("Upload", "Mbps", report.upload_mbps),
      stat("Ping (internet)", "ms", report.latency_ms),
      stat("Jitter", "ms", report.jitter_ms),
      h("div", { class: "stat" }, h("div", { class: "label" }, "Perda de pacotes"), h("div", { class: "value" }, one(report.loss_percent), h("small", null, "%")), h("div", { class: "range" }, "média do dia")),
      h("div", { class: "stat" }, h("div", { class: "label" }, "Quedas"), h("div", { class: "value" }, int(outages.length)),
        h("div", { class: "range" }, outages.length ? [`sem conexão por `, h("b", null, hours(report.downtime_ms)), h("br"), `às ${outages.map((o) => clock(o.start)).join(", ")}`] : "nenhuma queda"))),
  ];
}

function dayStrip(days, current) {
  return h("nav", { class: "days", "aria-label": "Escolher o dia" }, days.map((entry) => {
    const chip = dateChip(entry.day);
    return h("a", {
      href: `#/relatorios/${entry.day}`, "data-q": entry.grade || null,
      "aria-current": entry.day === current ? "page" : null,
      title: entry.headline || null,
    }, chip.weekday, h("i", { "aria-hidden": "true" }), chip.date,
    entry.grade && h("span", { class: "sr" }, ` nota ${qualityWord[qualityClass(entry.grade)]}`));
  }));
}

function bulletin(report, entries, settings, insights) {
  const position = entries.findIndex((e) => e.day === report.day);
  const number = position >= 0 ? entries.length - position : entries.length + 1;
  const prefs = settings.preferences;
  return [
    h("div", { class: "dateline" },
      h("span", null, `Boletim nº ${number} · ${dateLong(report.day)}`),
      h("span", null, `Perfil: ${report.profile_name}`, report.network && ` · ${report.network}`)),
    dayStrip(entries, report.day),
    hero(report),
    sectionTitle("Como foi cada parte do dia", h("p", null, "Mediana de cada período · download / upload · ping")),
    h("section", { class: "periods" }, report.periods.map(period)),
    speedSection(report, prefs.plan_download_mbps),
    latencySection(report),
    diagnosis(report),
    patternsSection(insights),
    numbers(report),
    planSection(insights),
    usageBar("Dados usados pelo AvaNet neste dia", report.data_used_bytes, prefs.fixed_daily_mb * 1e6, `${bytes(report.data_used_bytes)} de ${bytes(prefs.fixed_daily_mb * 1e6)}`),
  ].flat();
}

function notice(title, text) {
  return [h("div", { class: "notice" }, h("h1", null, title), h("p", null, text))];
}

// reportsView draws the bulletin of a day, or the most recent finished one.
export async function reportsView(container, day, onError) {
  try {
    const [index, settings] = await Promise.all([api.days(), api.settings()]);
    if (index.days.length === 0) {
      container.replaceChildren(...notice("Seu primeiro boletim vem aí", "Assim que o AvaNet tiver medições, o boletim aparece aqui. O de cada dia fica pronto às 8h da manhã seguinte."));
      return;
    }
    const selected = day || (index.days.find((d) => d.day !== index.today) ?? index.days[0]).day;
    let report, insights;
    try {
      [report, insights] = await Promise.all([api.report(selected), api.insights(selected)]);
    } catch (error) {
      if (error instanceof ApiError && (error.status === 404 || error.status === 400)) {
        container.replaceChildren(dayStrip(index.days, selected), ...notice("Sem dados neste dia", "O AvaNet não mediu nada nesta data. Escolha outro dia acima."));
        return;
      }
      throw error;
    }
    container.replaceChildren(...bulletin(report, index.days, settings, insights).filter(Boolean));
  } catch (error) {
    onError(error);
  }
}
