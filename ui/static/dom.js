// Builds the page. Values always become text nodes or attributes, never
// markup, so a Wi-Fi network's name cannot inject anything into the panel.

const SVG_NS = "http://www.w3.org/2000/svg";

function fill(node, props, children) {
  for (const [key, value] of Object.entries(props ?? {})) {
    if (value === null || value === undefined || value === false) continue;
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "value") node.value = value;
    else if (key === "checked") node.checked = Boolean(value);
    else node.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : String(child));
  }
  return node;
}

export const h = (tag, props, ...children) => fill(document.createElement(tag), props, children);
export const s = (tag, props, ...children) => fill(document.createElementNS(SVG_NS, tag), props, children);

// glyph builds a small line drawing from path data.
export function glyph(className, viewBox, ...paths) {
  return s("svg", {
    class: className, viewBox, fill: "none", stroke: "currentColor", "stroke-width": 2,
    "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true",
  }, paths.map((d) => s("path", { d })));
}
