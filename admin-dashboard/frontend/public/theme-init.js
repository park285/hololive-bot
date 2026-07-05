(function () {
  var pref = "system";
  try {
    var stored = localStorage.getItem("theme");
    if (stored === "light" || stored === "dark" || stored === "system") pref = stored;
  } catch (e) { }
  var dark = pref === "dark" || (pref === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
})();
