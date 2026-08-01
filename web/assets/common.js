// 右下角一键回顶部/到底部 + 下拉框搜索筛选。
(function () {
  var topBtn = document.querySelector('[data-scroll="top"]');
  var bottomBtn = document.querySelector('[data-scroll="bottom"]');
  if (!topBtn || !bottomBtn) return;

  function toggle() {
    var show = window.scrollY > 200;
    topBtn.hidden = !show;
    bottomBtn.hidden = !show;
  }

  topBtn.addEventListener('click', function () {
    window.scrollTo({ top: 0, behavior: 'smooth' });
  });
  bottomBtn.addEventListener('click', function () {
    window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });
  });

  window.addEventListener('scroll', toggle, { passive: true });
  toggle();
})();

// Passkey 注册与登录。
(function () {
  var messages = (window.WenLogI18n && window.WenLogI18n.messages) || {};
  function t(key, fallback) { return messages[key] || fallback; }

  function isPotentiallyTrustworthyOrigin() {
    var protocol = window.location.protocol;
    var host = window.location.hostname;
    return protocol === 'https:' || host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1';
  }

  var supported = !!(window.PublicKeyCredential && navigator.credentials && isPotentiallyTrustworthyOrigin());
  document.querySelectorAll('[data-passkey-supported]').forEach(function (el) {
    el.hidden = !supported;
  });
  if (!supported) return;

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? (meta.getAttribute('content') || '') : '';
  }

  function status(text) {
    var el = document.querySelector('[data-passkey-status]');
    if (el) el.textContent = text || '';
  }

  function b64ToBuf(value) {
    if (!value) return new ArrayBuffer(0);
    var s = String(value).replace(/-/g, '+').replace(/_/g, '/');
    while (s.length % 4) s += '=';
    var bin = atob(s);
    var buf = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
  }

  function bufToB64(buf) {
    if (!buf) return '';
    var bytes = new Uint8Array(buf);
    var s = '';
    for (var i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  }

  function normalizeCreationOptions(options) {
    var publicKey = options.publicKey || options.Response || options.response || options;
    publicKey.challenge = b64ToBuf(publicKey.challenge);
    if (publicKey.user && publicKey.user.id) publicKey.user.id = b64ToBuf(publicKey.user.id);
    (publicKey.excludeCredentials || []).forEach(function (cred) { cred.id = b64ToBuf(cred.id); });
    return publicKey;
  }

  function normalizeRequestOptions(options) {
    var publicKey = options.publicKey || options.Response || options.response || options;
    publicKey.challenge = b64ToBuf(publicKey.challenge);
    (publicKey.allowCredentials || []).forEach(function (cred) { cred.id = b64ToBuf(cred.id); });
    return publicKey;
  }

  function serializeCreate(cred) {
    return {
      id: cred.id,
      rawId: bufToB64(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment,
      response: {
        clientDataJSON: bufToB64(cred.response.clientDataJSON),
        attestationObject: bufToB64(cred.response.attestationObject)
      },
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {}
    };
  }

  function serializeGet(cred) {
    return {
      id: cred.id,
      rawId: bufToB64(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment,
      response: {
        clientDataJSON: bufToB64(cred.response.clientDataJSON),
        authenticatorData: bufToB64(cred.response.authenticatorData),
        signature: bufToB64(cred.response.signature),
        userHandle: bufToB64(cred.response.userHandle)
      },
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {}
    };
  }

  function postJSON(url, body, withCSRF) {
    var headers = { 'Content-Type': 'application/json' };
    if (withCSRF) headers['X-CSRF-Token'] = csrfToken();
    return fetch(url, { method: 'POST', headers: headers, body: body ? JSON.stringify(body) : '{}' })
      .then(function (r) { return r.json().then(function (data) { if (!r.ok || data.ok === false) throw new Error(data.message || t('requestFailed', 'Request failed')); return data; }); });
  }

  document.querySelectorAll('[data-passkey-register]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var nameInput = document.querySelector('[data-passkey-name]');
      var name = nameInput ? nameInput.value.trim() : '';
      if (!name) {
        status(t('passkeyNameRequired', 'Please enter a memorable Passkey name first.'));
        if (nameInput) nameInput.focus();
        return;
      }
      btn.disabled = true;
      status(t('passkeyCreating', 'Creating Passkey...'));
      postJSON('/admin/profile/passkeys/begin', {}, true)
        .then(function (data) { return navigator.credentials.create({ publicKey: normalizeCreationOptions(data.options) }); })
        .then(function (cred) { return postJSON('/admin/profile/passkeys/finish?name=' + encodeURIComponent(name), serializeCreate(cred), true); })
        .then(function () { status(t('passkeyAdded', 'Passkey added.')); window.location.reload(); })
        .catch(function (err) { status(err.message || t('passkeyFailed', 'Passkey operation failed.')); })
        .finally(function () { btn.disabled = false; });
    });
  });

  document.querySelectorAll('[data-passkey-login]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var selector = btn.getAttribute('data-passkey-username');
      var usernameInput = selector ? document.querySelector(selector) : null;
      var username = usernameInput ? usernameInput.value.trim() : '';
      btn.disabled = true;
      status(t('passkeyVerifying', 'Please verify with Passkey...'));
      postJSON('/auth/passkey/begin?username=' + encodeURIComponent(username), {}, false)
        .then(function (data) { return navigator.credentials.get({ publicKey: normalizeRequestOptions(data.options) }); })
        .then(function (cred) { return postJSON('/auth/passkey/finish', serializeGet(cred), false); })
        .then(function (data) { window.location.href = data.redirect || '/admin/'; })
        .catch(function (err) { status(err.message || t('passkeyLoginFailed', 'Passkey login failed.')); })
        .finally(function () { btn.disabled = false; });
    });
  });
})();

// 下拉框搜索筛选（跨主题通用）
(function () {
  var messages = (window.WenLogI18n && window.WenLogI18n.messages) || {};
  function t(key, fallback) { return messages[key] || fallback; }

  document.querySelectorAll("select[data-searchable-select]:not([multiple])").forEach(function (select, index) {
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
    input.placeholder = select.dataset.searchPlaceholder || t("searchPlaceholder", "Type to filter...");

    var list = document.createElement("div");
    list.className = "searchable-select-list";
    list.setAttribute("role", "listbox");
    var listboxId = select.name ? ("searchable-select-" + select.name + "-" + index) : ("searchable-select-" + index);
    list.id = listboxId;
    trigger.setAttribute("aria-controls", listboxId);

    var empty = document.createElement("div");
    empty.className = "searchable-select-empty";
    empty.hidden = true;
    empty.textContent = select.dataset.searchEmpty || t("searchEmpty", "No matches");

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
})();
