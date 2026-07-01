// Sets the theme before first paint so you never see a flash of the wrong one.
// Runs synchronously from <head>, and stays same-origin so the strict CSP
// (script-src 'self') is happy.
(function () {
  try {
    var stored = localStorage.getItem("redpaste-theme"); // "light" | "dark" | "system" | null
    var pref = stored || "system";
    var dark =
      pref === "dark" ||
      (pref === "system" &&
        window.matchMedia &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    var root = document.documentElement;
    root.setAttribute("data-theme", dark ? "dark" : "light");
    root.style.colorScheme = dark ? "dark" : "light";
  } catch (e) {
    document.documentElement.setAttribute("data-theme", "light");
  }
})();
