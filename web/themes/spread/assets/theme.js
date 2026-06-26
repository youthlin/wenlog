(function () {
  var navToggle = document.querySelector('[data-nav-toggle]');
  var navPanel = document.querySelector('[data-nav-panel]');
  if (navToggle && navPanel) {
    function setNavOpen(open) {
      navPanel.classList.toggle('is-open', open);
      document.body.classList.toggle('nav-is-open', open);
      navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
      navToggle.setAttribute('aria-label', open ? document.documentElement.dataset.navCloseLabel : document.documentElement.dataset.navOpenLabel);
      navToggle.textContent = open ? '×' : '☰';
    }

    navToggle.addEventListener('click', function (event) {
      event.stopPropagation();
      setNavOpen(!navPanel.classList.contains('is-open'));
    });

    navPanel.addEventListener('click', function (event) {
      event.stopPropagation();
    });

    document.addEventListener('click', function () {
      if (navPanel.classList.contains('is-open')) {
        setNavOpen(false);
      }
    });

    document.addEventListener('keydown', function (event) {
      if (event.key === 'Escape' && navPanel.classList.contains('is-open')) {
        setNavOpen(false);
      }
    });

    navPanel.addEventListener('click', function (event) {
      if (event.target.closest('a')) {
        setNavOpen(false);
      }
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

    function headingScrollOffset() {
      var header = document.querySelector('.site-header');
      return (header ? header.getBoundingClientRect().height : 0) + 16;
    }

    function scrollToHeading(heading, behavior) {
      if (!heading) {
        return;
      }
      var top = heading.getBoundingClientRect().top + window.pageYOffset - headingScrollOffset();
      window.scrollTo({ top: Math.max(0, top), behavior: behavior || 'auto' });
    }

    function currentHashTarget() {
      if (!window.location.hash) {
        return null;
      }
      try {
        return document.getElementById(decodeURIComponent(window.location.hash.slice(1)));
      } catch (error) {
        return null;
      }
    }

    function scrollToCurrentHash() {
      var target = currentHashTarget();
      if (target) {
        scrollToHeading(target, 'auto');
      }
    }

    window.requestAnimationFrame(scrollToCurrentHash);
    window.addEventListener('load', scrollToCurrentHash);
    window.setTimeout(scrollToCurrentHash, 250);
    window.setTimeout(scrollToCurrentHash, 750);

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

      tocList.addEventListener('click', function (event) {
        var link = event.target.closest('.toc-link');
        if (link) {
          var target = document.querySelector(link.hash);
          if (target) {
            event.preventDefault();
            history.pushState(null, '', link.hash);
            scrollToHeading(target, 'smooth');
          }
        }
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
