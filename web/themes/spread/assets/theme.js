(function () {
  var navToggle = document.querySelector('[data-nav-toggle]');
  var navPanel = document.querySelector('[data-nav-panel]');
  if (navToggle && navPanel) {
    navToggle.addEventListener('click', function () {
      var open = navPanel.classList.toggle('is-open');
      navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
      navToggle.setAttribute('aria-label', open ? document.documentElement.dataset.navCloseLabel : document.documentElement.dataset.navOpenLabel);
    });
  }

  var toc = document.querySelector('[data-toc]');
  var tocList = document.querySelector('[data-toc-list]');
  var content = document.querySelector('.article-body .post-content');
  if (toc && tocList && content) {
    var headings = Array.prototype.slice.call(content.querySelectorAll('h2, h3'));
    if (headings.length === 0) {
      toc.hidden = true;
      return;
    }
    headings.forEach(function (heading, index) {
      if (!heading.id) {
        heading.id = 'section-' + (index + 1);
      }
      var link = document.createElement('a');
      link.href = '#' + heading.id;
      link.textContent = heading.textContent || heading.id;
      link.className = 'toc-link toc-' + heading.tagName.toLowerCase();
      tocList.appendChild(link);
    });
  }
})();
