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
  var tocToggle = document.querySelector('[data-toc-toggle]');
  if (toc && tocList && content) {
    var headings = Array.prototype.slice.call(content.querySelectorAll('h2, h3'));
    if (headings.length === 0) {
      toc.hidden = true;
      if (tocToggle) {
        tocToggle.hidden = true;
      }
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

    if (tocToggle) {
      var mobileQuery = window.matchMedia('(max-width: 900px)');

      function closeToc() {
        toc.classList.remove('is-open');
        tocToggle.setAttribute('aria-expanded', 'false');
      }

      function syncTocMode() {
        tocToggle.hidden = !mobileQuery.matches;
        if (!mobileQuery.matches) {
          toc.classList.remove('is-open');
          tocToggle.setAttribute('aria-expanded', 'false');
        }
      }

      tocToggle.addEventListener('click', function (event) {
        event.stopPropagation();
        if (!mobileQuery.matches) {
          return;
        }
        var open = toc.classList.toggle('is-open');
        tocToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
      });

      toc.addEventListener('click', function (event) {
        event.stopPropagation();
      });

      document.addEventListener('click', function () {
        if (mobileQuery.matches) {
          closeToc();
        }
      });

      document.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') {
          closeToc();
        }
      });

      headings.forEach(function (heading) {
        heading.addEventListener('click', function () {
          if (mobileQuery.matches) {
            closeToc();
          }
        });
      });

      tocList.addEventListener('click', function () {
        if (mobileQuery.matches) {
          closeToc();
        }
      });

      if (typeof mobileQuery.addEventListener === 'function') {
        mobileQuery.addEventListener('change', syncTocMode);
      } else if (typeof mobileQuery.addListener === 'function') {
        mobileQuery.addListener(syncTocMode);
      }

      syncTocMode();
    }
  }
})();
