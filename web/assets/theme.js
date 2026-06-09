// 颜色主题切换 + 移动端汉堡菜单。
(function () {
  var themes = ["dark", "light", "red-plum", "bamboo", "lotus", "orange", "azure", "purple"];
  var defaultTheme = "dark";
  var themeStorageKey = "theme";

  function normalize(theme) {
    return themes.indexOf(theme) >= 0 ? theme : defaultTheme;
  }

  function setTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    if (document.body) {
      document.body.setAttribute("data-theme", theme);
    }
  }

  function syncThemeSelects(theme) {
    document.querySelectorAll("[data-theme-select]").forEach(function (select) {
      select.value = theme;
    });
  }

  function apply(theme) {
    theme = normalize(theme);
    setTheme(theme);
    try { localStorage.setItem(themeStorageKey, theme); } catch (e) {}
    syncThemeSelects(theme);
  }

  apply(document.documentElement.getAttribute("data-theme") || defaultTheme);

  document.querySelectorAll("[data-theme-select]").forEach(function (select) {
    select.value = normalize(document.documentElement.getAttribute("data-theme"));
    function handleThemeChange() {
      apply(select.value);
    }
    select.addEventListener("input", handleThemeChange);
    select.addEventListener("change", handleThemeChange);
  });

  window.addEventListener("pageshow", function () {
    var storedTheme = defaultTheme;
    try { storedTheme = normalize(localStorage.getItem(themeStorageKey)); } catch (e) {}
    setTheme(storedTheme);
    syncThemeSelects(storedTheme);
  });

  var navBtn = document.querySelector("[data-nav-toggle]");
  var navPanel = document.querySelector("[data-nav-panel]");
  var openLabel = document.documentElement.dataset.navOpenLabel || "Open menu";
  var closeLabel = document.documentElement.dataset.navCloseLabel || "Collapse menu";
  function isMobileNav() {
    return !!(navBtn && window.getComputedStyle(navBtn).display !== "none");
  }
  if (navBtn && navPanel) {
    function syncNavButton(open) {
      navBtn.setAttribute("aria-expanded", open ? "true" : "false");
      navBtn.setAttribute("aria-label", open ? closeLabel : openLabel);
      navBtn.textContent = open ? "✕" : "☰";
    }

    navBtn.addEventListener("click", function () {
      var open = navPanel.classList.toggle("is-open");
      syncNavButton(open);
    });

    navPanel.querySelectorAll("a").forEach(function (link) {
      link.addEventListener("click", function () {
        if (!isMobileNav()) {
          return;
        }
        navPanel.classList.remove("is-open");
        syncNavButton(false);
      });
    });

    window.addEventListener("resize", function () {
      if (!isMobileNav()) {
        navPanel.classList.remove("is-open");
        syncNavButton(false);
      }
    });

    syncNavButton(navPanel.classList.contains("is-open"));
  }
})();
