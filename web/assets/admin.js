// 后台交互:Markdown 编辑增强、预览、草稿、图片上传/粘贴/文件库插入、评论行内编辑、批量全选。
(function () {
  var root = document.documentElement.dataset || {};
  var MSG_UPLOADING = root.uploading || "Uploading...";
  var MSG_UPLOAD_FAILED = root.uploadFailed || "Upload failed";
  var MSG_IMAGE_INSERTED = root.imageInserted || "Image inserted";
  var MSG_EXISTING_IMAGE_INSERTED = root.existingImageInserted || "Existing image inserted";
  var MSG_LOAD_FILES_FAILED = root.loadFilesFailed || "Failed to load files";
  var MSG_EXPAND = root.expandLabel || "展开";
  var MSG_COLLAPSE = root.collapseLabel || "收起";

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? (meta.getAttribute("content") || "") : "";
  }

  function triggerInput(el) {
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function insertAtCursor(textarea, text, selectStartOffset, selectEndOffset) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var value = textarea.value;
    textarea.value = value.slice(0, start) + text + value.slice(end);
    var pos = start + text.length;
    textarea.selectionStart = textarea.selectionEnd = pos;
    if (typeof selectStartOffset === "number" && typeof selectEndOffset === "number") {
      textarea.selectionStart = start + selectStartOffset;
      textarea.selectionEnd = start + selectEndOffset;
    }
    textarea.focus();
    triggerInput(textarea);
  }

  function selectedText(textarea) {
    return textarea.value.slice(textarea.selectionStart || 0, textarea.selectionEnd || 0);
  }

  function replaceSelection(textarea, text, selectStartOffset, selectEndOffset) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var value = textarea.value;
    textarea.value = value.slice(0, start) + text + value.slice(end);
    textarea.selectionStart = start + (selectStartOffset || text.length);
    textarea.selectionEnd = start + (selectEndOffset || text.length);
    textarea.focus();
    triggerInput(textarea);
  }

  function wrapSelection(textarea, before, after, placeholder) {
    var sel = selectedText(textarea);
    var text = sel || placeholder;
    replaceSelection(textarea, before + text + after, before.length, before.length + text.length);
  }

  function prefixLines(textarea, prefix, fallback) {
    var sel = selectedText(textarea) || fallback;
    var lines = sel.split("\n");
    var text = lines.map(function (line, i) {
      return (typeof prefix === "function" ? prefix(i) : prefix) + line;
    }).join("\n");
    replaceSelection(textarea, text);
  }

  function markdownAction(textarea, action) {
    switch (action) {
    case "heading":
      prefixLines(textarea, "## ", "小标题");
      break;
    case "bold":
      wrapSelection(textarea, "**", "**", "加粗文本");
      break;
    case "italic":
      wrapSelection(textarea, "*", "*", "斜体文本");
      break;
    case "quote":
      prefixLines(textarea, "> ", "引用内容");
      break;
    case "ul":
      prefixLines(textarea, "- ", "列表项");
      break;
    case "ol":
      prefixLines(textarea, function (i) { return (i + 1) + ". "; }, "列表项");
      break;
    case "code":
      wrapSelection(textarea, "`", "`", "code");
      break;
    case "codeblock":
      wrapSelection(textarea, "```\n", "\n```", "code");
      break;
    case "link": {
      var sel = selectedText(textarea) || "链接文字";
      var url = window.prompt("链接 URL", "https://");
      if (url === null) return;
      replaceSelection(textarea, "[" + sel + "](" + url + ")");
      break;
    }
    case "more":
      insertAtCursor(textarea, "\n<!--more-->\n");
      break;
    case "hr":
      insertAtCursor(textarea, "\n---\n");
      break;
    }
  }

  function setStatus(text) {
    var tip = document.getElementById("upload_status");
    if (tip) tip.textContent = text;
  }

  function uploadImage(file, textarea, onDone) {
    var fd = new FormData();
    fd.set("file", file);
    setStatus(MSG_UPLOADING);
    fetch("/admin/upload", { method: "POST", body: fd, headers: { "X-CSRF-Token": csrfToken() } })
      .then(function (r) { return r.json(); })
      .then(function (data) {
        if (!data.ok) throw new Error(data.message || MSG_UPLOAD_FAILED);
        if (textarea) insertAtCursor(textarea, data.markdown + "\n");
        setStatus(MSG_IMAGE_INSERTED);
        if (onDone) onDone(data);
      })
      .catch(function (err) {
        setStatus("");
        alert(err.message || MSG_UPLOAD_FAILED);
      });
  }

  function renderUploadPicker(items, textarea) {
    var list = document.getElementById("upload_picker_list");
    if (!list) return;
    list.innerHTML = "";
    items.forEach(function (item) {
      var btn = document.createElement("button");
      var img = document.createElement("img");
      var span = document.createElement("span");
      btn.type = "button";
      btn.className = "upload-pick-item";
      img.src = item.Path;
      img.alt = "";
      span.textContent = item.OrigName || item.Path;
      btn.appendChild(img);
      btn.appendChild(span);
      btn.addEventListener("click", function () {
        insertAtCursor(textarea, "![](" + item.Path + ")\n");
        document.getElementById("upload_picker").hidden = true;
        setStatus(MSG_EXISTING_IMAGE_INSERTED);
      });
      list.appendChild(btn);
    });
  }

  function loadUploadPicker(textarea) {
    fetch("/admin/uploads.json?page=1")
      .then(function (r) { return r.json(); })
      .then(function (data) {
        if (!data.ok) throw new Error(data.message || MSG_LOAD_FILES_FAILED);
        renderUploadPicker(data.uploads || [], textarea);
        document.getElementById("upload_picker").hidden = false;
      })
      .catch(function (err) { alert(err.message || MSG_LOAD_FILES_FAILED); });
  }

  function debounce(fn, wait) {
    var timer;
    return function () {
      clearTimeout(timer);
      var args = arguments;
      timer = setTimeout(function () { fn.apply(null, args); }, wait);
    };
  }

  function normalizeSlug(text, mode) {
    var value = String(text || "").trim().toLowerCase();
    var out = "";
    var pendingDash = false;
    for (var i = 0; i < value.length; i++) {
      var ch = value.charAt(i);
      if (/\s/.test(ch)) {
        pendingDash = out.length > 0;
        continue;
      }
      if (ch === "-" || ch === "_" || (mode === "page" && ch === ".") || /[a-z0-9]/.test(ch) || /[\p{L}\p{N}]/u.test(ch)) {
        if (pendingDash && out.charAt(out.length - 1) !== "-" && ch !== "-" && ch !== "_") out += "-";
        out += ch;
        pendingDash = false;
      }
    }
    out = out.replace(/^-+|-+$/g, "");
    if (mode === "page") out = out.replace(/^[^a-z0-9]+/i, "");
    return out;
  }

  function initSlugSync() {
    var title = document.querySelector("[data-slug-source]");
    var slug = document.querySelector("[data-slug-target]");
    if (!title || !slug) return;
    var mode = slug.getAttribute("data-slug-mode") || "post";
    var auto = !slug.value;
    slug.addEventListener("input", function () { auto = !slug.value; });
    title.addEventListener("input", function () {
      if (!auto) return;
      slug.value = normalizeSlug(title.value, mode);
      triggerInput(slug);
    });
    slug.addEventListener("blur", function () {
      var normalized = normalizeSlug(slug.value, mode);
      if (normalized !== slug.value) {
        slug.value = normalized;
        triggerInput(slug);
      }
    });
  }

  function initCategoryRequired() {
    var box = document.querySelector("[data-category-required]");
    if (!box) return;
    var form = box.closest("form");
    var message = box.getAttribute("data-category-required-message") || "Please select at least one category.";
    var inputs = Array.prototype.slice.call(box.querySelectorAll('input[name="category_ids"]'));
    if (!form || inputs.length === 0) return;
    function setValidity() {
      var checked = inputs.some(function (input) { return input.checked; });
      inputs[0].setCustomValidity(checked ? "" : message);
    }
    inputs.forEach(function (input) { input.addEventListener("change", setValidity); });
    form.addEventListener("submit", function () {
      setValidity();
      if (!inputs.some(function (input) { return input.checked; })) inputs[0].reportValidity();
    });
    setValidity();
  }

  var ta = document.getElementById("content_md");
  var editor = document.querySelector("[data-md-editor]");
  var pv = document.getElementById("md_preview");
  var fileInput = document.getElementById("upload_file");
  var pickBtn = document.getElementById("pick_upload_btn");
  var closePickerBtn = document.getElementById("close_upload_picker");
  var currentView = "edit";

  function renderMarkdownPreview() {
    if (!ta || !pv) return;
    var body = new URLSearchParams();
    body.set("content_md", ta.value);
    fetch("/admin/preview", { method: "POST", body: body, headers: { "X-CSRF-Token": csrfToken() } })
      .then(function (r) { return r.text(); })
      .then(function (html) { pv.innerHTML = html; });
  }

  var debouncedPreview = debounce(function () {
    if (currentView !== "edit") renderMarkdownPreview();
  }, 450);

  function setMarkdownView(view) {
    if (!editor) return;
    currentView = view;
    editor.classList.toggle("is-edit", view === "edit");
    editor.classList.toggle("is-split", view === "split");
    editor.classList.toggle("is-preview", view === "preview");
    document.querySelectorAll("[data-md-view]").forEach(function (btn) {
      btn.classList.toggle("active", btn.getAttribute("data-md-view") === view);
    });
    if (view !== "edit") renderMarkdownPreview();
  }

  function draftKey() {
    var form = document.querySelector('form.edit-form[action="/admin/post"]');
    if (!form) return "";
    var id = (form.querySelector('input[name="id"]') || {}).value || "new";
    var type = (form.querySelector('input[name="post_type"]') || {}).value || "post";
    return "wenlog:post-draft:" + type + ":" + id;
  }

  function initDraftRestore() {
    if (!ta) return;
    var key = draftKey();
    if (!key || !window.localStorage) return;
    var box = document.getElementById("draft_restore");
    var restoreBtn = document.getElementById("restore_draft_btn");
    var discardBtn = document.getElementById("discard_draft_btn");
    var status = document.getElementById("draft_status");
    var saved = localStorage.getItem(key);
    if (saved && saved !== ta.value && box) box.hidden = false;
    if (restoreBtn) restoreBtn.addEventListener("click", function () {
      ta.value = localStorage.getItem(key) || ta.value;
      if (box) box.hidden = true;
      triggerInput(ta);
      ta.focus();
    });
    if (discardBtn) discardBtn.addEventListener("click", function () {
      localStorage.removeItem(key);
      if (box) box.hidden = true;
      if (status) status.textContent = "已忽略本地草稿。";
    });
    var saveDraft = debounce(function () {
      localStorage.setItem(key, ta.value);
      if (status) status.textContent = "已自动保存到本地。";
    }, 800);
    ta.addEventListener("input", saveDraft);
    var form = ta.closest("form");
    if (form) form.addEventListener("submit", function () { localStorage.removeItem(key); });
  }

  if (ta) {
    document.querySelectorAll("[data-md-action]").forEach(function (btn) {
      btn.addEventListener("click", function () { markdownAction(ta, btn.getAttribute("data-md-action")); });
    });
    document.querySelectorAll("[data-md-view]").forEach(function (btn) {
      btn.addEventListener("click", function () { setMarkdownView(btn.getAttribute("data-md-view")); });
    });
    ta.addEventListener("input", debouncedPreview);
    ta.addEventListener("keydown", function (e) {
      var mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      var key = String(e.key || "").toLowerCase();
      if (key === "b") { e.preventDefault(); markdownAction(ta, "bold"); }
      if (key === "i") { e.preventDefault(); markdownAction(ta, "italic"); }
      if (key === "k") { e.preventDefault(); markdownAction(ta, "link"); }
      if (key === "s") {
        e.preventDefault();
        var form = ta.closest("form");
        if (form && form.requestSubmit) {
          form.requestSubmit();
        } else if (form) {
          var keyName = draftKey();
          if (keyName && window.localStorage) localStorage.removeItem(keyName);
          form.submit();
        }
      }
    });
    initDraftRestore();
  }

  initSlugSync();
  initCategoryRequired();

  if (ta && fileInput) {
    fileInput.addEventListener("change", function () {
      if (fileInput.files && fileInput.files[0]) uploadImage(fileInput.files[0], ta);
      fileInput.value = "";
    });
    ta.addEventListener("paste", function (e) {
      var items = (e.clipboardData && e.clipboardData.items) || [];
      for (var i = 0; i < items.length; i++) {
        var it = items[i];
        if (it.kind === "file" && it.type.indexOf("image/") === 0) {
          e.preventDefault();
          var file = it.getAsFile();
          if (file) uploadImage(file, ta);
          return;
        }
      }
    });
    ta.addEventListener("dragover", function (e) { e.preventDefault(); });
    ta.addEventListener("drop", function (e) {
      var files = (e.dataTransfer && e.dataTransfer.files) || [];
      if (files[0] && files[0].type.indexOf("image/") === 0) {
        e.preventDefault();
        uploadImage(files[0], ta);
      }
    });
    if (pickBtn) pickBtn.addEventListener("click", function () { loadUploadPicker(ta); });
    if (closePickerBtn) closePickerBtn.addEventListener("click", function () { document.getElementById("upload_picker").hidden = true; });
  }

  var pageUpload = document.getElementById("upload_file_page");
  if (pageUpload) {
    pageUpload.addEventListener("change", function () {
      if (pageUpload.files && pageUpload.files[0]) {
        uploadImage(pageUpload.files[0], null, function () { window.location.reload(); });
      }
      pageUpload.value = "";
    });
  }

  document.querySelectorAll(".c-edit-btn").forEach(function (btn) {
    var cell = btn.closest(".editable-cell");
    if (!cell) return;
    var text = cell.querySelector(".c-display");
    var form = cell.querySelector(".c-edit");
    if (!cell || !text || !form) return;
    btn.addEventListener("click", function () {
      text.hidden = true;
      form.hidden = false;
      btn.hidden = true;
    });
    form.querySelector(".c-edit-cancel").addEventListener("click", function () {
      text.hidden = false;
      form.hidden = true;
      btn.hidden = false;
    });
  });

  document.querySelectorAll(".c-reply-btn").forEach(function (btn) {
    var cell = btn.closest(".editable-cell");
    if (!cell) return;
    var form = cell.querySelector(".c-reply");
    if (!form) return;
    btn.addEventListener("click", function () {
      form.hidden = false;
      btn.hidden = true;
      var textarea = form.querySelector("textarea");
      if (textarea) textarea.focus();
    });
    var cancel = form.querySelector(".c-reply-cancel");
    if (cancel) {
      cancel.addEventListener("click", function () {
        form.hidden = true;
        btn.hidden = false;
      });
    }
  });

  document.querySelectorAll("[data-comment-body]").forEach(function (body) {
    var toggle = body.parentElement ? body.parentElement.querySelector("[data-comment-toggle]") : null;
    if (!toggle) return;
    requestAnimationFrame(function () {
      if (body.scrollHeight <= body.clientHeight + 1) return;
      toggle.hidden = false;
      toggle.textContent = MSG_EXPAND;
    });
    toggle.addEventListener("click", function () {
      var expanded = body.classList.toggle("is-expanded");
      toggle.textContent = expanded ? MSG_COLLAPSE : MSG_EXPAND;
    });
  });

  var checkAll = document.getElementById("check-all");
  if (checkAll) {
    checkAll.addEventListener("change", function () {
      document.querySelectorAll(".row-check").forEach(function (cb) {
        cb.checked = checkAll.checked;
      });
    });
  }
})();
