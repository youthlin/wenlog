// 评论 Ajax 提交 + 评论分页 Ajax 切换。
(function () {
  var root = document.documentElement.dataset || {};
  var MSG_SUCCESS = root.commentSuccess || "Comment submitted and awaiting moderation.";
  var MSG_FAIL = root.commentFail || "Submission failed. Please try again later.";
  var MSG_NETWORK = root.commentNetwork || "Network error. Please try again later.";
  var commentsBox = document.getElementById("comments-box");
  var form = document.getElementById("comment-form");
  if (!commentsBox) return;

  function getForm() { return document.getElementById("comment-form"); }
  function getFormBox() { return document.getElementById("comment-form-box") || getForm(); }
  function getFormHome() { return document.getElementById("comment-form-home"); }

  function moveFormHome() {
    var f = getForm();
    var box = getFormBox();
    var home = getFormHome();
    if (!f || !box || !home || !home.parentNode) return;
    home.parentNode.insertBefore(box, home.nextSibling);
    f.querySelector("[name=parent_id]").value = "0";
    f.querySelector("[name=reply_to_id]").value = "0";
    var cancel = f.querySelector("[data-cancel-reply]");
    if (cancel) cancel.hidden = true;
  }

  function moveFormToComment(commentID, replyToID) {
    var f = getForm();
    var box = getFormBox();
    var target = document.getElementById(commentID);
    if (!f || !box || !target) return;
    target.appendChild(box);
    f.querySelector("[name=parent_id]").value = replyToID;
    f.querySelector("[name=reply_to_id]").value = replyToID;
    var cancel = f.querySelector("[data-cancel-reply]");
    if (cancel) cancel.hidden = false;
    box.scrollIntoView({ behavior: "smooth", block: "center" });
    var textarea = f.querySelector("textarea[name=content]");
    if (textarea) textarea.focus({ preventScroll: true });
  }

  function showMsg(text, ok) {
    var f = getForm();
    if (!f) return;
    var el = f.querySelector(".comment-msg");
    if (!el) {
      el = document.createElement("div");
      el.className = "comment-msg";
      f.appendChild(el);
    }
    el.textContent = text;
    el.className = "comment-msg " + (ok ? "ok" : "err");
  }

  function insertTextAtCursor(textarea, text) {
    if (!textarea) return;
    var start = textarea.selectionStart;
    var end = textarea.selectionEnd;
    var value = textarea.value || "";
    if (typeof start !== "number" || typeof end !== "number") {
      textarea.value = value + text;
      return;
    }
    textarea.value = value.slice(0, start) + text + value.slice(end);
    var next = start + text.length;
    textarea.setSelectionRange(next, next);
  }

  function bindSmilies() {
    var f = getForm();
    if (!f) return;
    var textarea = f.querySelector("textarea[name=content]");
    if (!textarea) return;
    f.querySelectorAll("[data-smiley-code]").forEach(function (btn) {
      if (btn.dataset.boundSmiley === "1") return;
      btn.dataset.boundSmiley = "1";
      btn.addEventListener("click", function () {
        var code = btn.getAttribute("data-smiley-code") || "";
        if (!code) return;
        textarea.focus({ preventScroll: true });
        insertTextAtCursor(textarea, code);
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
      });
    });
  }

  function bindReplyButtons() {
    var f = getForm();
    if (!f) return;
    commentsBox.querySelectorAll("[data-reply]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        moveFormToComment(btn.getAttribute("data-reply-target"), btn.getAttribute("data-reply"));
      });
    });
  }

  function fetchCommentPage(page, push) {
    var url = new URL(window.location.href);
    url.searchParams.set("cpage", page);
    url.searchParams.set("fragment", "comments");
    return fetch(url.toString(), { headers: { "X-Requested-With": "XMLHttpRequest" } })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        commentsBox.innerHTML = html;
        bindReplyButtons();
        bindForm();
        var comments = document.getElementById("comments");
        if (comments) comments.scrollIntoView({ behavior: "smooth" });
        if (push) {
          var u = new URL(window.location.href);
          u.searchParams.set("cpage", page);
          u.searchParams.delete("ajax");
          if (page === 1) u.searchParams.delete("cpage");
          if (!u.hash) u.hash = "comments";
          history.pushState({ cpage: page }, "", u.toString());
        }
      });
  }

  function bindPager() {
    commentsBox.querySelectorAll(".comment-pagination a[data-cpage]").forEach(function (a) {
      a.addEventListener("click", function (e) {
        e.preventDefault();
        var page = parseInt(a.getAttribute("data-cpage"), 10) || 1;
        fetchCommentPage(page, true);
      });
    });
  }

  function bindForm() {
    var f = getForm();
    if (!f || f.dataset.bound === "1") return;
    bindSmilies();
    f.dataset.bound = "1";
    f.addEventListener("submit", function (e) {
      e.preventDefault();
      var btn = f.querySelector("button[type=submit]");
      btn.disabled = true;
      fetch("/comment", {
        method: "POST",
        headers: { "X-Requested-With": "XMLHttpRequest" },
        body: new FormData(f),
      })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          if (data.ok) {
            var page = parseInt(data.comment_page, 10) || (parseInt(new URL(window.location.href).searchParams.get("cpage"), 10) || 1);
            f.querySelector("[name=content]").value = "";
            return fetchCommentPage(page, true).then(function () {
              showMsg(data.message || MSG_SUCCESS, true);
            });
          } else {
            showMsg(data.message || MSG_FAIL, false);
          }
        })
        .catch(function () { showMsg(MSG_NETWORK, false); })
        .finally(function () { btn.disabled = false; });
    });
    var cancel = f.querySelector("[data-cancel-reply]");
    if (cancel) {
      cancel.addEventListener("click", moveFormHome);
    }
    bindReplyButtons();
    bindPager();
  }

  window.addEventListener("popstate", function () {
    var url = new URL(window.location.href);
    var page = parseInt(url.searchParams.get("cpage"), 10) || 1;
    fetchCommentPage(page, false);
  });

  bindForm();
  bindPager();
})();
