// Applies the saved theme before the page paints, so it does not flash.
"use strict";
try {
  const saved = localStorage.getItem("avanet-theme");
  if (saved === "light" || saved === "dark") document.documentElement.dataset.theme = saved;
} catch {
  // Storage can be blocked; the theme then simply follows Windows.
}
