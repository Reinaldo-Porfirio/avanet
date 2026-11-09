// Pieces shared by the pages.

import { glyph, h } from "./dom.js";
import { qualityClass, qualityWord } from "./format.js";

export const badge = (quality) => h("span", { class: `badge ${qualityClass(quality)}` }, qualityWord[qualityClass(quality)]);

export const clockLegend = () => h("figcaption", { class: "legend" },
  [["good", "Boa"], ["fair", "Regular"], ["poor", "Ruim"], ["none", "Sem leitura"]].map(([kind, label]) =>
    h("span", null, h("i", { class: kind }), label)));

export const sectionTitle = (title, detail) => h("div", { class: "section-title" }, h("h2", null, title), detail);

export const usageBar = (label, used, limit, text) => h("div", { class: "usage" },
  h("span", null, label),
  h("div", { class: "bar" }, widthBar(limit > 0 ? used / limit : 0)),
  h("span", { class: "num" }, text));

function widthBar(ratio) {
  const fill = h("span");
  // A CSS property set from script is allowed by the page's security policy;
  // an inline style attribute would not be.
  fill.style.width = `${Math.min(100, Math.round(ratio * 100))}%`;
  return fill;
}

const NODE_PATHS = {
  pc: ["M5 4h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z", "M8 20h8", "M12 16v4"],
  router: ["M5 13h14a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2z", "M7 16.5h.01", "M7 13V9", "M17 13V9", "M9.5 6.5a4 4 0 0 1 5 0"],
  web: ["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z", "M3 12h18", "M12 3a14 14 0 0 1 0 18", "M12 3a14 14 0 0 0 0 18"],
};

const node = (kind, label) => h("div", { class: "node" }, glyph("", "0 0 24 24", ...NODE_PATHS[kind]), label);

// chain draws the path from this computer to the internet. The text beside
// it says the same, so the meaning never depends on the drawing alone.
export const chain = ({ near, far }) => h("div", { class: "chain", "aria-hidden": "true" },
  node("pc", "Seu PC"),
  h("div", { class: `link ${near.quality}` }, h("small", null, near.label)),
  node("router", "Roteador"),
  h("div", { class: `link ${far.quality}` }, h("small", null, far.label)),
  node("web", "Internet"));
