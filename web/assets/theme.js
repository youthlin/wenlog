// 颜色主题切换 + 移动端汉堡菜单。
(function () {
  var themes = ["dark", "light", "red-plum", "bamboo", "lotus", "orange", "azure", "purple"];
  var defaultTheme = "dark";

  function normalize(theme) {
    return themes.indexOf(theme) >= 0 ? theme : defaultTheme;
  }

  function apply(theme) {
    theme = normalize(theme);
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem("theme", theme); } catch (e) {}
    document.querySelectorAll("[data-theme-select]").forEach(function (select) {
      select.value = theme;
    });
  }

  apply(document.documentElement.getAttribute("data-theme") || defaultTheme);

  document.querySelectorAll("[data-theme-select]").forEach(function (select) {
    select.value = normalize(document.documentElement.getAttribute("data-theme"));
    select.addEventListener("change", function () {
      apply(select.value);
    });
  });

  var navBtn = document.querySelector("[data-nav-toggle]");
  var navPanel = document.querySelector("[data-nav-panel]");
  var openLabel = document.documentElement.dataset.navOpenLabel || "Open menu";
  var closeLabel = document.documentElement.dataset.navCloseLabel || "Collapse menu";
  var themeToggleLabel = document.documentElement.dataset.themeToggleLabel || "Toggle theme";
  function isMobileNav() {
    return !!(navBtn && window.getComputedStyle(navBtn).display !== "none");
  }
  document.querySelectorAll("[data-theme-select]").forEach(function (select) {
    select.setAttribute("aria-label", themeToggleLabel);
  });
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
