// 暗黑模式切换 + 移动端汉堡菜单。
(function () {
  function apply(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem("theme", theme); } catch (e) {}
  }

  document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var cur = document.documentElement.getAttribute("data-theme") || "light";
      apply(cur === "dark" ? "light" : "dark");
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
  document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
    btn.setAttribute("aria-label", themeToggleLabel);
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
