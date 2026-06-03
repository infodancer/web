// Timeline embed loader
// Usage: <script src="https://your-timeline-host/static/embed.js" data-timeline="slug" data-height="600"></script>
(function() {
    var script = document.currentScript;
    if (!script) return;

    var slug = script.getAttribute('data-timeline');
    if (!slug) { console.error('timeline embed: data-timeline attribute required'); return; }

    var height = script.getAttribute('data-height') || '700';
    var base = script.src.replace(/\/static\/embed\.js.*$/, '');

    var iframe = document.createElement('iframe');
    iframe.src = base + '/t/' + slug + '?embed=true';
    iframe.style.cssText = 'width:100%;height:' + height + 'px;border:none;';
    iframe.setAttribute('loading', 'lazy');
    iframe.setAttribute('title', 'Timeline');

    script.parentNode.insertBefore(iframe, script.nextSibling);
})();
