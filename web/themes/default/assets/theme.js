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

  function initSearchableSelects() {
    document.querySelectorAll("select[data-searchable-select]").forEach(function (select, index) {
      if (select.dataset.searchableReady === "1") {
        return;
      }
      select.dataset.searchableReady = "1";

      var wrapper = document.createElement("div");
      wrapper.className = "searchable-select";
      if (select.dataset.searchableFullWidth !== undefined) {
        wrapper.classList.add("is-full-width");
      }

      var trigger = document.createElement("button");
      trigger.type = "button";
      trigger.className = "searchable-select-trigger";
      trigger.setAttribute("aria-haspopup", "listbox");

      var triggerLabel = document.createElement("span");
      triggerLabel.className = "searchable-select-trigger-label";
      trigger.appendChild(triggerLabel);

      var triggerIcon = document.createElement("span");
      triggerIcon.className = "searchable-select-trigger-icon";
      triggerIcon.setAttribute("aria-hidden", "true");
      triggerIcon.textContent = "▾";
      trigger.appendChild(triggerIcon);

      var panel = document.createElement("div");
      panel.className = "searchable-select-panel";
      panel.hidden = true;

      var input = document.createElement("input");
      input.type = "search";
      input.className = "searchable-select-input";
      input.placeholder = select.dataset.searchPlaceholder || "输入关键字筛选…";

      var list = document.createElement("div");
      list.className = "searchable-select-list";
      list.setAttribute("role", "listbox");
      var listboxId = select.name ? ("searchable-select-" + select.name + "-" + index) : ("searchable-select-" + index);
      list.id = listboxId;
      trigger.setAttribute("aria-controls", listboxId);

      var empty = document.createElement("div");
      empty.className = "searchable-select-empty";
      empty.hidden = true;
      empty.textContent = select.dataset.searchEmpty || "没有匹配项";

      panel.appendChild(input);
      panel.appendChild(list);
      panel.appendChild(empty);

      select.parentNode.insertBefore(wrapper, select.nextSibling);
      wrapper.appendChild(trigger);
      wrapper.appendChild(panel);
      select.classList.add("searchable-select-native");
      select.hidden = true;

      function currentOption() {
        return select.options[select.selectedIndex] || select.options[0] || null;
      }

      function syncTriggerLabel() {
        var option = currentOption();
        triggerLabel.textContent = option ? option.textContent : "";
      }

      function closePanel() {
        wrapper.classList.remove("is-open");
        panel.hidden = true;
        trigger.setAttribute("aria-expanded", "false");
      }

      function openPanel() {
        wrapper.classList.add("is-open");
        panel.hidden = false;
        trigger.setAttribute("aria-expanded", "true");
        input.focus();
        input.select();
      }

      var activeIndex = -1;

      function visibleOptions() {
        return list.querySelectorAll(".searchable-select-option");
      }

      function setActive(index) {
        var opts = visibleOptions();
        opts.forEach(function (opt, i) {
          var isActive = i === index;
          opt.classList.toggle("is-active", isActive);
          opt.setAttribute("aria-selected", isActive ? "true" : "false");
        });
        if (index >= 0 && index < opts.length) {
          list.setAttribute("aria-activedescendant", opts[index].id);
          opts[index].scrollIntoView({ block: "nearest" });
        } else {
          list.removeAttribute("aria-activedescendant");
        }
      }

      function selectActive() {
        var opts = visibleOptions();
        if (activeIndex >= 0 && activeIndex < opts.length) {
          opts[activeIndex].click();
        }
      }

      function renderOptions(keyword) {
        var lower = (keyword || "").trim().toLowerCase();
        list.innerHTML = "";
        activeIndex = -1;
        list.removeAttribute("aria-activedescendant");
        var matched = 0;
        Array.prototype.forEach.call(select.options, function (option, optIdx) {
          if (!option || option.disabled) {
            return;
          }
          var text = (option.textContent || "").trim();
          if (lower && text.toLowerCase().indexOf(lower) < 0) {
            return;
          }
          matched += 1;
          var item = document.createElement("button");
          item.type = "button";
          item.className = "searchable-select-option";
          item.id = listboxId + "-opt-" + optIdx;
          if (option.selected) {
            item.classList.add("is-active");
            item.setAttribute("aria-selected", "true");
            activeIndex = matched - 1;
          } else {
            item.setAttribute("aria-selected", "false");
          }
          item.textContent = text;
          item.addEventListener("click", function () {
            select.value = option.value;
            syncTriggerLabel();
            closePanel();
            select.dispatchEvent(new Event("input", { bubbles: true }));
            select.dispatchEvent(new Event("change", { bubbles: true }));
          });
          list.appendChild(item);
        });
        if (activeIndex >= 0) {
          list.setAttribute("aria-activedescendant", listboxId + "-opt-" + select.selectedIndex);
        }
        empty.hidden = matched > 0;
      }

      trigger.addEventListener("click", function () {
        if (wrapper.classList.contains("is-open")) {
          closePanel();
          return;
        }
        renderOptions(input.value);
        openPanel();
      });

      input.addEventListener("input", function () {
        renderOptions(input.value);
      });

      input.addEventListener("keydown", function (event) {
        var opts = visibleOptions();
        switch (event.key) {
          case "Escape":
            closePanel();
            trigger.focus();
            break;
          case "ArrowDown":
            event.preventDefault();
            if (opts.length > 0) {
              activeIndex = activeIndex < opts.length - 1 ? activeIndex + 1 : 0;
              setActive(activeIndex);
            }
            break;
          case "ArrowUp":
            event.preventDefault();
            if (opts.length > 0) {
              activeIndex = activeIndex > 0 ? activeIndex - 1 : opts.length - 1;
              setActive(activeIndex);
            }
            break;
          case "Enter":
            event.preventDefault();
            selectActive();
            break;
          case "Home":
            event.preventDefault();
            if (opts.length > 0) {
              activeIndex = 0;
              setActive(activeIndex);
            }
            break;
          case "End":
            event.preventDefault();
            if (opts.length > 0) {
              activeIndex = opts.length - 1;
              setActive(activeIndex);
            }
            break;
        }
      });

      document.addEventListener("click", function (event) {
        if (!wrapper.contains(event.target)) {
          closePanel();
        }
      });

      select.addEventListener("change", function () {
        syncTriggerLabel();
        if (wrapper.classList.contains("is-open")) {
          renderOptions(input.value);
        }
      });

      syncTriggerLabel();
      renderOptions("");
      trigger.setAttribute("aria-expanded", "false");
    });
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

  initSearchableSelects();

  var navBtn = document.querySelector("[data-nav-toggle]");
  var navPanel = document.querySelector("[data-nav-panel]");
  var navBackdrop = document.querySelector("[data-nav-backdrop]");
  var openLabel = document.documentElement.dataset.navOpenLabel || "Open menu";
  var closeLabel = document.documentElement.dataset.navCloseLabel || "Collapse menu";
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
})();
