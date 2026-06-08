// 评论 Ajax 提交 + 评论分页 Ajax 切换。
(function () {
  var commentsBox = document.getElementById("comments-box");
  var form = document.getElementById("comment-form");
  if (!commentsBox) return;

  function getForm() { return document.getElementById("comment-form"); }

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

  function bindReplyButtons() {
    var f = getForm();
    if (!f) return;
    commentsBox.querySelectorAll("[data-reply]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        f.querySelector("[name=parent_id]").value = btn.getAttribute("data-reply");
        f.scrollIntoView({ behavior: "smooth" });
      });
    });
  }

  function fetchCommentPage(page, push) {
    var url = new URL(window.location.href);
    url.searchParams.set("cpage", page);
    url.searchParams.set("ajax", "comments");
    fetch(url.toString(), { headers: { "X-Requested-With": "XMLHttpRequest" } })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        commentsBox.innerHTML = html;
        bindReplyButtons();
        bindForm();
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
            showMsg(data.message || "评论已提交,等待审核后显示。", true);
            f.querySelector("[name=content]").value = "";
            f.querySelector("[name=parent_id]").value = "0";
          } else {
            showMsg(data.message || "提交失败,请稍后重试。", false);
          }
        })
        .catch(function () { showMsg("网络错误,请稍后重试。", false); })
        .finally(function () { btn.disabled = false; });
    });
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
