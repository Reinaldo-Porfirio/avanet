// Chooses the page from the address and handles what all pages share.

import { SessionExpired } from "./api.js";
import { h } from "./dom.js";
import { nowView } from "./view-now.js";
import { reportsView } from "./view-reports.js";
import { settingsView } from "./view-settings.js";

const main = document.getElementById("conteudo");
const TITLES = { agora: "Agora", relatorios: "Relatórios", ajustes: "Ajustes" };
const DAY = /^\d{4}-\d{2}-\d{2}$/;
let stopCurrent = null;

function showError(error) {
  const expired = error instanceof SessionExpired;
  main.replaceChildren(h("div", { class: "notice" },
    h("h1", null, expired ? "Esta aba expirou" : "O AvaNet não respondeu"),
    h("p", null, expired
      ? "Por segurança, cada abertura do painel é válida até o AvaNet fechar. Abra de novo pelo ícone perto do relógio do Windows."
      : "Confira se o AvaNet ainda está aberto (ícone perto do relógio do Windows) e tente de novo.")));
}

function parseRoute() {
  const [, tab = "agora", day = ""] = location.hash.split("/");
  return TITLES[tab] ? { tab, day: DAY.test(day) ? day : "" } : { tab: "agora", day: "" };
}

function route() {
  stopCurrent?.();
  stopCurrent = null;
  const { tab, day } = parseRoute();

  document.title = `${TITLES[tab]} · AvaNet`;
  for (const link of document.querySelectorAll("[data-tab]")) {
    if (link.dataset.tab === tab) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  }

  if (tab === "agora") stopCurrent = nowView(main, showError);
  else if (tab === "relatorios") reportsView(main, day, showError);
  else settingsView(main, showError);
  window.scrollTo(0, 0);
}

document.getElementById("theme").addEventListener("click", () => {
  const root = document.documentElement;
  const dark = root.dataset.theme ? root.dataset.theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  root.dataset.theme = dark ? "light" : "dark";
  try {
    localStorage.setItem("avanet-theme", root.dataset.theme);
  } catch {
    // Storage can be blocked; the choice then lasts only until the page closes.
  }
});

window.addEventListener("hashchange", route);
route();
