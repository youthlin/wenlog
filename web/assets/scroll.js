// 右下角一键回顶部/到底部。
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
