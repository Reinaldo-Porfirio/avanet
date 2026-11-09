// "Agora": how the connection is right now.

import { ApiError, api } from "./api.js";
import { clock24 } from "./charts.js";
import { h } from "./dom.js";
import { bytes, clock, hours, hourOfDay, int, one, qualityWord, short } from "./format.js";
import { badge, chain, clockLegend, sectionTitle, usageBar } from "./parts.js";

const POLL_MS = 15000;
const POLL_AFTER_REQUEST_MS = 3000;

function headline(now) {
  const ping = `${int(now.latency_ms)} ms`;
  if (!now.measured) return { text: "Medindo…", em: "", tone: "", end: "", lede: "A primeira medição está em andamento. Isso leva alguns segundos." };
  if (now.outage || !now.target) {
    return {
      text: "Sem ", em: "sinal", tone: "poor",
      lede: now.gateway_ms !== undefined
        ? "O roteador respondeu, mas a internet não. O problema parece estar depois dele, na operadora."
        : "A internet não respondeu e o roteador também não. Confira o Wi-Fi ou o cabo.",
    };
  }
  switch (now.quality) {
    case "good": return { text: "Céu limpo", em: "", tone: "", lede: `Sua internet está respondendo em ${ping}. Dá para navegar, assistir e fazer chamadas de vídeo sem susto.` };
    case "fair": return { text: "Algumas ", em: "nuvens", tone: "fair", lede: `Sua internet está respondendo em ${ping}, mais devagar que o ideal. Chamadas e jogos podem sentir.` };
    default: return { text: "Tempo ", em: "fechado", tone: "poor", lede: `Sua internet está respondendo em ${ping} e perdendo fôlego. Chamadas e jogos devem travar agora.` };
  }
}

function linkQuality(ms, fair, poor) {
  if (ms === undefined) return "unknown";
  if (ms > poor) return "poor";
  return ms > fair ? "fair" : "good";
}

function hero(now, report) {
  const title = headline(now);
  const covered = `Monitorado hoje: ${hours(now.monitored_today_ms)} de ${hours(now.elapsed_today_ms)}`;
  const hoursOfDay = report?.hours;
  return h("section", { class: "hero" },
    h("div", null,
      h("p", { class: "kicker" }, "Neste momento"),
      h("h1", null, title.text, title.em && h("em", { class: title.tone }, title.em), title.end ?? "."),
      h("p", { class: "lede" }, title.lede),
      h("div", { class: "verdict" }, badge(now.outage ? "poor" : now.quality), h("span", null, "para uso em geral")),
      h("p", { class: "coverage" }, covered, now.at && ` · última medição às ${clock(now.at)}`)),
    h("figure", { class: "clock" },
      clock24({
        hours: hoursOfDay, nowHour: now.at ? hourOfDay(now.at) : undefined,
        title: now.at ? clock(now.at) : "—", subtitle: qualityWord[now.quality]?.toLowerCase() ?? "",
      }),
      clockLegend()));
}

function welcome() {
  return h("div", { class: "banner" },
    h("div", null, h("strong", null, "Para que você mais usa a internet?"), h("span", null, "Escolher um perfil ajusta o que conta como bom ou ruim nos boletins.")),
    h("a", { class: "button", href: "#/ajustes" }, "Escolher perfil"));
}

function path(now) {
  return h("section", { class: "card diagnosis" },
    h("h3", null, "Do seu computador até a internet"),
    h("p", { class: "sub" }, now.connection || "Conexão ainda não identificada",
      now.signal_percent ? ` · sinal de ${int(now.signal_percent)}%` : ""),
    chain({
      near: { quality: linkQuality(now.gateway_ms, 20, 50), label: now.gateway_ms !== undefined ? `Rede · ${short(now.gateway_ms)} ms` : "Rede · sem resposta" },
      far: { quality: now.target ? linkQuality(now.latency_ms, 50, 100) : "poor", label: now.target ? `Internet · ${int(now.latency_ms)} ms` : "Internet · sem resposta" },
    }),
    h("p", null, pathExplanation(now)));
}

function pathExplanation(now) {
  if (!now.measured) return "Assim que a primeira medição terminar, este caminho aparece aqui.";
  if (now.gateway_ms === undefined && !now.target) return "Nem o roteador nem a internet responderam.";
  if (now.gateway_ms === undefined) return `A internet respondeu em ${int(now.latency_ms)} ms. O roteador não respondeu ao teste, o que alguns modelos fazem de propósito.`;
  if (!now.target) return `Seu roteador respondeu em ${short(now.gateway_ms)} ms, mas a internet não respondeu.`;
  return `Seu roteador respondeu em ${short(now.gateway_ms)} ms e a internet em ${int(now.latency_ms)} ms.`;
}

function speedAction(now, state, refresh) {
  const blocked = !now.data.allowed;
  const busy = now.speed_running || state.requested;
  const onclick = async () => {
    state.requested = true;
    refresh(false);
    try {
      await api.requestSpeedTest();
    } catch (error) {
      state.requested = false;
      state.error = error instanceof ApiError ? error.message : "Não foi possível pedir o teste.";
    }
    refresh(true, POLL_AFTER_REQUEST_MS);
  };
  let message = now.speed_notice || state.error || "";
  if (blocked) message = "Você está em dados móveis e os testes de velocidade estão desligados. Dá para ligar em Ajustes.";
  else if (busy) message = "O teste leva menos de um minuto e respeita seu limite de dados.";
  return h("div", { class: "speed-action" },
    h("button", { class: "button", type: "button", "data-focus": "speed", disabled: busy || blocked, onclick },
      busy ? "Medindo…" : "Testar velocidade agora"),
    message && h("p", { role: "status" }, message));
}

function numbers(now) {
  const speed = now.last_speed;
  const stat = (label, value, unit, detail) => h("div", { class: "stat" },
    h("div", { class: "label" }, label),
    h("div", { class: "value" }, value, unit && h("small", null, unit)),
    h("div", { class: "range" }, detail));
  return h("section", { class: "stats" },
    stat("Ping (internet)", now.target ? int(now.latency_ms) : "—", now.target ? "ms" : "", now.target ? `pelo servidor ${now.target}` : "sem resposta"),
    stat("Jitter", now.target ? short(now.jitter_ms) : "—", now.target ? "ms" : "", "variação do ping"),
    stat("Perda de pacotes", now.target ? one(now.loss_percent) : "—", now.target ? "%" : "", "na última medição"),
    stat("Download", speed ? int(speed.download_mbps) : "—", speed ? "Mbps" : "", speed ? `teste das ${clock(speed.at)}` : "nenhum teste ainda"),
    stat("Upload", speed ? int(speed.upload_mbps) : "—", speed ? "Mbps" : "", speed?.local_traffic ? "outros programas usavam a rede" : "último teste"),
    stat("Perfil", now.profile.name, "", h("a", { href: "#/ajustes" }, "trocar")));
}

function page(now, report, state, refresh) {
  return [
    h("div", { class: "dateline" }, h("span", null, "Agora"), h("span", null, `Perfil: ${now.profile.name}`, now.connection && ` · ${now.connection}`)),
    !now.profile_chosen && welcome(),
    hero(now, report),
    sectionTitle("O caminho da sua conexão", h("p", null, "Separa o que é da sua casa do que é da operadora")),
    path(now),
    sectionTitle("Os números de agora"),
    numbers(now),
    speedAction(now, state, refresh),
    usageBar(now.data.mobile ? "Dados móveis usados pelo AvaNet hoje" : "Dados usados pelo AvaNet hoje",
      now.data.used_bytes, now.data.limit_bytes, `${bytes(now.data.used_bytes)} de ${bytes(now.data.limit_bytes)}`),
  ];
}

async function loadToday(today) {
  try {
    return await api.report(today);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

// nowView draws the page and returns a function that stops its updates.
export function nowView(container, onError) {
  const state = { requested: false, error: "" };
  let timer = 0, stopped = false, lastKey = "";

  const render = (now, report) => {
    const focused = document.activeElement?.dataset?.focus;
    container.replaceChildren(...page(now, report, state, refresh).filter(Boolean));
    if (focused) container.querySelector(`[data-focus="${focused}"]`)?.focus();
  };

  let latest = null, latestReport = null;
  async function refresh(fetchData = true, nextIn = POLL_MS) {
    clearTimeout(timer);
    if (!fetchData) {
      if (latest) render(latest, latestReport);
      return;
    }
    try {
      const now = await api.now();
      if (stopped) return;
      if (now.speed_running || now.speed_notice) state.requested = false;
      if (state.requested && latest?.last_speed?.at !== now.last_speed?.at) state.requested = false;

      const { elapsed_today_ms: _e, monitored_today_ms: _m, ...stable } = now;
      const key = JSON.stringify([stable, state]);
      if (key !== lastKey) {
        latest = now;
        latestReport = await loadToday(now.today);
        if (stopped) return;
        lastKey = key;
        render(latest, latestReport);
      }
      if (document.visibilityState !== "hidden") timer = setTimeout(refresh, now.speed_running || state.requested ? POLL_AFTER_REQUEST_MS : nextIn);
    } catch (error) {
      if (!stopped) onError(error);
    }
  }

  const onVisible = () => { if (document.visibilityState === "visible") refresh(); };
  document.addEventListener("visibilitychange", onVisible);
  refresh();
  return () => {
    stopped = true;
    clearTimeout(timer);
    document.removeEventListener("visibilitychange", onVisible);
  };
}
