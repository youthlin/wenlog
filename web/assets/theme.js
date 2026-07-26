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

  var messages = (window.WenLogI18n && window.WenLogI18n.messages) || {};
  function t(key, fallback) { return messages[key] || fallback; }
  var navBtn = document.querySelector("[data-nav-toggle]");
  var navPanel = document.querySelector("[data-nav-panel]");
  var navBackdrop = document.querySelector("[data-nav-backdrop]");
  var openLabel = t("navOpenLabel", "Open menu");
  var closeLabel = t("navCloseLabel", "Collapse menu");
  function isMobileNav() {
    return !!(navBtn && window.getComputedStyle(navBtn).display !== "none");
  }
  if (navBtn && navPanel) {
    function setNavState(open) {
      navPanel.classList.toggle("is-open", open);
      if (navBackdrop) {
        navBackdrop.classList.toggle("is-open", open);
      }
      if (document.body) {
        document.body.classList.toggle("nav-open", open && isMobileNav());
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
        if (!isMobileNav()) {
          return;
        }
        setNavState(false);
      });
    });

    window.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        setNavState(false);
      }
    });

    window.addEventListener("resize", function () {
      if (!isMobileNav()) {
        setNavState(false);
      }
    });

    setNavState(navPanel.classList.contains("is-open"));
  }

  // 导航组折叠：默认全部折叠，仅当前活跃组展开。
  (function () {
    var navGroups = document.querySelectorAll(".admin-nav .nav-group");
    if (!navGroups.length) return;

    // 找到包含 is-active 链接的组并展开。
    navGroups.forEach(function (group) {
      if (group.querySelector(".admin-nav-link.is-active")) {
        group.classList.add("is-expanded");
      }
    });

    // 点击组标题切换展开/折叠。
    navGroups.forEach(function (group) {
      var title = group.querySelector(".nav-group-title");
      if (!title) return;
      title.addEventListener("click", function () {
        group.classList.toggle("is-expanded");
      });
    });
  })();
})();
