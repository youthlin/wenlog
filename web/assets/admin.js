// 后台交互:Markdown 预览、图片上传/粘贴/文件库插入、评论行内编辑、批量全选。
  (function () {
  var root = document.documentElement.dataset || {};
  var MSG_UPLOADING = root.uploading || "Uploading...";
  var MSG_UPLOAD_FAILED = root.uploadFailed || "Upload failed";
  var MSG_IMAGE_INSERTED = root.imageInserted || "Image inserted";
  var MSG_EXISTING_IMAGE_INSERTED = root.existingImageInserted || "Existing image inserted";
  var MSG_LOAD_FILES_FAILED = root.loadFilesFailed || "Failed to load files";
  var LABEL_PREVIEW = root.previewLabel || "Preview";
  var LABEL_CONTINUE_EDITING = root.continueEditingLabel || "Continue editing";

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? (meta.getAttribute("content") || "") : "";
  }

  function insertAtCursor(textarea, text) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var value = textarea.value;
    textarea.value = value.slice(0, start) + text + value.slice(end);
    var pos = start + text.length;
    textarea.selectionStart = textarea.selectionEnd = pos;
    textarea.focus();
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

  function setStatus(text) {
    var tip = document.getElementById("upload_status");
    if (tip) tip.textContent = text;
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
        insertAtCursor(textarea, '![](' + item.Path + ')\n');
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

  var previewBtn = document.getElementById("preview_btn");
  var ta = document.getElementById("content_md");
  var fileInput = document.getElementById("upload_file");
  var pickBtn = document.getElementById("pick_upload_btn");
  var closePickerBtn = document.getElementById("close_upload_picker");
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

  if (previewBtn) {
    var pv = document.getElementById("md_preview");
    previewBtn.addEventListener("click", function () {
      if (!pv.hidden) {
        pv.hidden = true;
        ta.hidden = false;
        previewBtn.textContent = LABEL_PREVIEW;
        return;
      }
      var body = new URLSearchParams();
      body.set("content_md", ta.value);
      fetch("/admin/preview", { method: "POST", body: body, headers: { "X-CSRF-Token": csrfToken() } })
        .then(function (r) { return r.text(); })
        .then(function (html) {
          pv.innerHTML = html;
          pv.hidden = false;
          ta.hidden = true;
          previewBtn.textContent = LABEL_CONTINUE_EDITING;
        });
    });
  }

  document.querySelectorAll(".c-edit-btn").forEach(function (btn) {
    var cell = btn.closest(".editable-cell");
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

  var checkAll = document.getElementById("check-all");
  if (checkAll) {
    checkAll.addEventListener("change", function () {
      document.querySelectorAll(".row-check").forEach(function (cb) {
        cb.checked = checkAll.checked;
      });
    });
  }
})();
