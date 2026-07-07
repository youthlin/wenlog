// Twenty Twenty 主题：暗色模式切换 + 移动端汉堡菜单。
(function () {
  var themes = ["light", "dark"];
  var defaultTheme = "light";
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

  // 移动端汉堡菜单
  var navBtn = document.querySelector("[data-nav-toggle]");
  var navPanel = document.querySelector("[data-nav-panel]");
  var navBackdrop = document.querySelector("[data-nav-backdrop]");
  var siteHeader = document.querySelector(".site-header");
  var messages = (window.WenLogI18n && window.WenLogI18n.messages) || {};
  function t(key, fallback) { return messages[key] || fallback; }
  var openLabel = t("navOpenLabel", "Open menu");
  var closeLabel = t("navCloseLabel", "Collapse menu");
  function isMobileNav() {
    return !!(navBtn && window.getComputedStyle(navBtn).display !== "none");
  }
  if (navBtn && navPanel) {
    function syncMobileNavOffset() {
      if (!siteHeader || !document.documentElement) return;
      document.documentElement.style.setProperty("--mobile-nav-offset", siteHeader.offsetHeight + "px");
    }

    function setNavState(open) {
      navPanel.classList.toggle("is-open", open);
      if (navBackdrop) {
        navBackdrop.hidden = !open;
        navBackdrop.classList.toggle("is-open", open);
      }
      if (document.body) {
        document.body.classList.toggle("nav-open", open && isMobileNav());
      }
      if (open) {
        syncMobileNavOffset();
      }
      syncNavButton(open);
    }

    function syncNavButton(open) {
      navBtn.setAttribute("aria-expanded", open ? "true" : "false");
      navBtn.setAttribute("aria-label", open ? closeLabel : openLabel);
      navBtn.textContent = open ? "✕" : "☰";
    }

    navBtn.addEventListener("click", function () {
      setNavState(!navPanel.classList.contains("is-open"));
    });

    if (navBackdrop) {
      navBackdrop.addEventListener("click", function () {
        setNavState(false);
      });
    }

    navPanel.querySelectorAll("a").forEach(function (link) {
      link.addEventListener("click", function () {
        if (!isMobileNav()) return;
        setNavState(false);
      });
    });

    window.addEventListener("keydown", function (event) {
      if (event.key === "Escape") setNavState(false);
    });

    window.addEventListener("resize", function () {
      syncMobileNavOffset();
      if (!isMobileNav()) setNavState(false);
    });

    syncMobileNavOffset();
    setNavState(navPanel.classList.contains("is-open"));
  }
})();
