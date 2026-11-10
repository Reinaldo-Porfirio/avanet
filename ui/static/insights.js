// What several days say together: recurring hours and the contracted plan.

import { h } from "./dom.js";
import { clock, dateChip, int } from "./format.js";
import { sectionTitle } from "./parts.js";

const MIN_PATTERN_DAYS = 3;
const CONFIDENCE = { low: [1, "baixa"], medium: [2, "média"], high: [3, "alta"] };

const dayLabel = (day) => dateChip(day).date;

function confidence(level) {
  const [dots, word] = CONFIDENCE[level] ?? CONFIDENCE.low;
  return h("div", { class: "confidence" },
    "Confiança ",
    h("span", { class: "dots", "aria-hidden": "true" }, [1, 2, 3].map((i) => h("i", { class: i <= dots ? "on" : null }))),
    ` ${word}`);
}

function patternCard(pattern) {
  const bad = pattern.kind === "bad";
  return h("article", { class: bad ? "card forecast" : "card" },
    h("h3", null, bad ? "Padrão provável" : "Seu melhor horário"),
    h("p", { class: "sub" }, `Visto em ${pattern.hits} de ${pattern.days} dias`),
    h("blockquote", { class: "pattern-text" }, pattern.text),
    confidence(pattern.confidence));
}

export function patternsSection(insights) {
  const { days, patterns } = insights.patterns;
  let content;
  if (patterns.length > 0) {
    content = h("section", { class: "split" }, patterns.map(patternCard));
  } else if (days < MIN_PATTERN_DAYS) {
    const missing = MIN_PATTERN_DAYS - days;
    content = h("p", { class: "card empty-note" },
      `Com ${MIN_PATTERN_DAYS} dias de medições, o AvaNet começa a apontar os horários que se repetem. `,
      missing === 1 ? "Falta 1 dia." : `Faltam ${missing} dias.`);
  } else {
    content = h("p", { class: "card empty-note" }, "Nenhum horário se repetiu nesses dias: o que aconteceu foi pontual.");
  }
  return [
    sectionTitle("Padrões dos últimos dias", h("p", null, `${days} ${days === 1 ? "dia" : "dias"} com medições até ${dayLabel(insights.until)}`)),
    content,
  ];
}

// bar shows the plan as the full track, the references as marks, the average
// as the fill and the lowest test as a tick. The numbers are written beside it.
function bar(metric, reference) {
  const fill = h("span", { class: "fill" });
  fill.style.width = `${Math.min(100, metric.average_percent)}%`;
  const lowest = h("span", { class: "lowest" });
  lowest.style.left = `${Math.min(100, metric.minimum_percent)}%`;
  const mark = (percent) => {
    const node = h("span", { class: "mark" });
    node.style.left = `${percent}%`;
    return node;
  };
  return h("div", { class: "plan-bar", "aria-hidden": "true" },
    fill, mark(reference.average), mark(reference.minimum), lowest);
}

function metricCard(label, metric, plan, reference, enough) {
  const ok = metric.meets_average && metric.meets_minimum;
  return h("article", { class: "card plan-metric" },
    h("div", { class: "label" }, label),
    h("div", { class: "value" }, int(metric.average_percent), h("small", null, "% do plano na média")),
    bar(metric, reference),
    h("p", { class: "plan-numbers" },
      `Média de ${int(metric.average_mbps)} Mbps de ${int(plan)} contratados. Menor teste: ${int(metric.minimum_mbps)} Mbps `,
      `(${int(metric.minimum_percent)}%) em ${dayLabel(metric.minimum_at.slice(0, 10))} às ${clock(metric.minimum_at)}.`),
    enough && h("p", { class: `plan-verdict ${ok ? "good" : "poor"}` },
      ok ? "Dentro da referência" : verdictText(metric, reference)));
}

function verdictText(metric, reference) {
  const parts = [];
  if (!metric.meets_average) parts.push(`média abaixo de ${reference.average}%`);
  if (!metric.meets_minimum) parts.push(`${metric.below_minimum} ${metric.below_minimum === 1 ? "teste abaixo" : "testes abaixo"} de ${reference.minimum}%`);
  return `Fora da referência: ${parts.join(" e ")}`;
}

export function planSection(insights) {
  const title = sectionTitle("Você recebe o que contratou?", h("p", null, `Últimos 30 dias até ${dayLabel(insights.until)}`));
  const plan = insights.plan;
  if (!plan) {
    return [title, h("div", { class: "banner" },
      h("div", null, h("strong", null, "Informe a velocidade do seu plano"), h("span", null, "Assim o AvaNet compara o que você paga com o que chega de verdade.")),
      h("a", { class: "button", href: "#/ajustes" }, "Abrir Ajustes"))];
  }
  if (plan.tests === 0) {
    return [title, h("p", { class: "card empty-note" }, "Ainda não há testes de velocidade nesses dias para comparar com o plano.")];
  }

  const reference = { average: plan.reference_average_percent, minimum: plan.reference_minimum_percent };
  const notes = [];
  if (!plan.enough) notes.push(`Ainda são poucos testes para uma conclusão: ${plan.tests} em ${plan.days} ${plan.days === 1 ? "dia" : "dias"}. São precisos pelo menos 10, em 3 dias diferentes.`);
  if (plan.via_wifi) notes.push("A maioria dos testes foi pelo Wi-Fi, que tem limites próprios e pode explicar parte da diferença. Para comparar com rigor, use o cabo.");
  if (plan.capped) notes.push("Alguns testes chegaram ao limite de dados por teste. Em planos muito rápidos, a velocidade pode ter sido subestimada.");

  return [
    title,
    h("section", { class: "split" },
      plan.download && metricCard("Download", plan.download, plan.plan_download_mbps, reference, plan.enough),
      plan.upload && metricCard("Upload", plan.upload, plan.plan_upload_mbps, reference, plan.enough)),
    h("div", { class: "plan-notes" },
      h("p", { class: "plan-legend" },
        h("span", null, h("i", { class: "plan-key fill" }), "média"),
        h("span", null, h("i", { class: "plan-key mark" }), `referências de ${reference.average}% e ${reference.minimum}%`),
        h("span", null, h("i", { class: "plan-key lowest" }), "menor teste")),
      notes.map((note) => h("p", null, note)),
      h("p", null,
        `A referência de ${reference.average}% na média e ${reference.minimum}% no mínimo é a que a Anatel divulga ao consumidor. `,
        "Esta comparação é indicativa e não serve como prova: para reclamar, use o medidor oficial indicado pela Anatel (brasilbandalarga.com.br), de preferência com o computador no cabo.")),
  ];
}

const rangeWords = ({ from, to }) => `${from === 0 ? "da meia-noite" : `das ${from}h`} ${to === 24 ? "à meia-noite" : `às ${to}h`}`;

// forecast turns a recurring problem into a warning for today.
export function forecast(insights) {
  const pattern = insights?.patterns.patterns.find((p) => p.kind === "bad");
  if (!pattern) return null;
  return h("div", { class: "banner forecast-banner" },
    h("div", null,
      h("strong", null, `Previsão: atenção ${rangeWords(pattern.range)}`),
      h("span", null, pattern.text)),
    h("a", { class: "button quiet", href: "#/relatorios" }, "Ver boletins"));
}
