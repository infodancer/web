(function() {
    'use strict';

    var container = document.querySelector('.page-timeline');
    if (!container) return;

    var slug = container.dataset.slug;
    var wrap = container.querySelector('.tl-wrap');
    var eventList = document.getElementById('event-list');
    var sentinelTop = document.getElementById('sentinel-top');
    var sentinelBottom = document.getElementById('sentinel-bottom');

    // Parse lane config
    var lanes = JSON.parse(eventList.dataset.lanes || '[]');
    var laneMap = {};
    lanes.forEach(function(l) { laneMap[l.id] = l; });

    var visibleCategories = {};
    lanes.forEach(function(l) { visibleCategories[l.id] = true; });

    var loading = false;
    var reachedTop = false;
    var reachedBottom = false;
    var activeTag = 0; // 0 = no tag filter

    // ---- Grid computation ----

    // Returns { gridTemplate, axisCol, leftGapCol, rightGapCol, yearSpan, leftCols[], rightCols[] }
    // Grid layout: [left lanes...] [left-gap 24px] [axis 2px] [right-gap 24px] [right lanes...]
    function computeGrid() {
        var leftLanes = [];
        var rightLanes = [];
        lanes.forEach(function(l) {
            if (!visibleCategories[l.id]) return;
            if (l.side === 'left') leftLanes.push(l);
            else rightLanes.push(l);
        });
        leftLanes.sort(function(a, b) { return a.index - b.index; });
        rightLanes.sort(function(a, b) { return a.index - b.index; });

        var lc = Math.max(leftLanes.length, 1);
        var rc = Math.max(rightLanes.length, 1);

        // Columns: [lc x 1fr] [24px gap] [2px axis] [24px gap] [rc x 1fr]
        var parts = [];
        for (var i = 0; i < lc; i++) parts.push('1fr');
        parts.push('24px');  // left gap
        parts.push('2px');   // axis
        parts.push('24px');  // right gap
        for (var j = 0; j < rc; j++) parts.push('1fr');

        var gridTemplate = parts.join(' ');
        var leftGapCol = lc + 1;
        var axisCol = lc + 2;
        var rightGapCol = lc + 3;

        var leftCols = {};
        leftLanes.forEach(function(l) {
            leftCols[l.id] = lc - l.index; // index 0 = nearest axis = column lc
        });

        var rightCols = {};
        rightLanes.forEach(function(l) {
            rightCols[l.id] = rightGapCol + 1 + l.index; // index 0 = nearest axis
        });

        return {
            gridTemplate: gridTemplate,
            axisCol: axisCol,
            leftGapCol: leftGapCol,
            rightGapCol: rightGapCol,
            totalCols: lc + 3 + rc,
            leftCols: leftCols,
            rightCols: rightCols,
            lc: lc,
            rc: rc
        };
    }

    function getEventCol(grid, lane) {
        if (!lane) return { col: grid.rightGapCol + 1, side: 'right' };
        if (lane.side === 'left' && grid.leftCols[lane.id]) {
            return { col: grid.leftCols[lane.id], side: 'left' };
        }
        if (lane.side === 'right' && grid.rightCols[lane.id]) {
            return { col: grid.rightCols[lane.id], side: 'right' };
        }
        return { col: lane.side === 'left' ? 1 : grid.rightGapCol + 1, side: lane.side };
    }

    // ---- Axis stripe positioning ----
    function positionAxisStripe(grid) {
        var body = container.querySelector('.tl-body');
        if (!body) return;
        // The axis column is grid.axisCol in a grid on .tl-lane-bar.
        // For the body stripe, we compute the position from the bar.
        var bar = container.querySelector('.tl-lane-bar');
        if (!bar) return;
        var barRect = bar.getBoundingClientRect();
        var axisEl = bar.querySelector('.tl-lane-bar-axis');
        if (!axisEl) return;
        var axisRect = axisEl.getBoundingClientRect();
        var leftOffset = axisRect.left - barRect.left;
        body.style.position = 'relative';
        // Use a real element instead of pseudoelement for reliability
        var stripe = body.querySelector('.tl-axis-stripe');
        if (!stripe) {
            stripe = document.createElement('div');
            stripe.className = 'tl-axis-stripe';
            stripe.style.cssText = 'position:absolute;top:0;bottom:0;width:2px;background:' +
                getComputedStyle(document.documentElement).getPropertyValue('--axis-color').trim() +
                ';z-index:0;';
            body.insertBefore(stripe, body.firstChild);
        }
        stripe.style.left = leftOffset + 'px';
    }

    // ---- Layout ----
    function layoutEvents() {
        var grid = computeGrid();

        // Set grid on wrap
        wrap.style.setProperty('--tl-grid', grid.gridTemplate);
        wrap.style.setProperty('--axis-col', grid.axisCol);

        // Update lane bar — make it a flat grid matching the event grid
        var bar = container.querySelector('.tl-lane-bar');
        if (bar) {
            bar.style.gridTemplateColumns = grid.gridTemplate;
            bar.querySelectorAll('.tl-lane-label').forEach(function(label) {
                var catId = parseInt(label.dataset.categoryId, 10);
                if (!visibleCategories[catId]) {
                    label.style.display = 'none';
                    return;
                }
                label.style.display = '';
                var side = label.dataset.side;
                // Place label in its grid column
                if (side === 'left' && grid.leftCols[catId]) {
                    label.style.gridColumn = grid.leftCols[catId];
                } else if (side === 'right' && grid.rightCols[catId]) {
                    label.style.gridColumn = grid.rightCols[catId];
                }
            });
            // Axis element spans gap + axis + gap
            var axisEl = bar.querySelector('.tl-lane-bar-axis');
            if (axisEl) axisEl.style.gridColumn = grid.leftGapCol + ' / ' + (grid.rightGapCol + 1);
        }

        // Remove old markers and gaps
        eventList.querySelectorAll('.tl-year-marker, .tl-gap').forEach(function(el) { el.remove(); });

        var events = eventList.querySelectorAll('.tl-event');
        var prevDate = null;
        var prevYear = null;

        events.forEach(function(evt) {
            var catId = parseInt(evt.dataset.categoryId, 10);
            var lane = laneMap[catId] || laneMap[0];
            var dateStr = evt.dataset.date;
            var year = dateStr ? dateStr.substring(0, 4) : null;

            // Visibility
            if (!visibleCategories[catId]) {
                evt.classList.add('evt-hidden');
                return;
            }
            evt.classList.remove('evt-hidden');

            // Place the title in the correct grid column
            var pos = getEventCol(grid, lane);
            var color = lane ? lane.color : '#6366f1';
            var titleEl = evt.querySelector('.tl-event-title');
            if (titleEl) {
                titleEl.style.gridColumn = pos.col;
                titleEl.style.setProperty('--evt-color', color);
                titleEl.classList.toggle('evt-title-left', pos.side === 'left');
            }

            // Ensure axis dot exists
            var dot = evt.querySelector('.tl-event-axis');
            if (!dot) {
                dot = document.createElement('div');
                dot.className = 'tl-event-axis';
                evt.appendChild(dot);
            }
            dot.style.gridColumn = grid.axisCol;
            dot.style.setProperty('--evt-color', color);

            // Connector line from axis to card
            var conn = evt.querySelector('.tl-event-connector');
            if (!conn) {
                conn = document.createElement('div');
                conn.className = 'tl-event-connector';
                evt.appendChild(conn);
            }
            conn.style.setProperty('--evt-color', color);
            if (pos.side === 'left') {
                // Connector spans from card's right edge through gap to axis
                conn.style.gridColumn = (pos.col + 1) + ' / ' + (grid.axisCol + 1);
            } else {
                // Connector spans from axis through gap to card's left edge
                conn.style.gridColumn = grid.axisCol + ' / ' + pos.col;
            }

            // Year marker
            if (year && year !== prevYear && prevYear !== null) {
                insertYearMarker(evt, year, grid);
            }

            // Time gap
            if (prevDate && dateStr) {
                var gap = computeGap(prevDate, dateStr);
                if (gap > 0) {
                    var spacer = document.createElement('div');
                    spacer.className = 'tl-gap';
                    spacer.style.height = gap + 'px';
                    var prev = evt.previousElementSibling;
                    if (prev && prev.classList.contains('tl-year-marker')) {
                        prev.parentNode.insertBefore(spacer, prev);
                    } else {
                        evt.parentNode.insertBefore(spacer, evt);
                    }
                }
            }

            if (dateStr) { prevDate = dateStr; prevYear = year; }
        });

        // First year marker
        var firstEvt = eventList.querySelector('.tl-event:not(.evt-hidden)');
        if (firstEvt && firstEvt.dataset.date) {
            var firstYear = firstEvt.dataset.date.substring(0, 4);
            var prev = firstEvt.previousElementSibling;
            if (!prev || !prev.classList.contains('tl-year-marker')) {
                insertYearMarker(firstEvt, firstYear, grid);
            }
        }

        // Position axis stripe after layout
        requestAnimationFrame(function() { positionAxisStripe(grid); });
    }

    function insertYearMarker(beforeEl, year, grid) {
        var marker = document.createElement('div');
        marker.className = 'tl-year-marker';

        var label = document.createElement('div');
        label.className = 'tl-year-marker-label';
        // Span across left-gap + axis + right-gap for centering
        label.style.gridColumn = grid.leftGapCol + ' / ' + (grid.rightGapCol + 1);
        label.innerHTML = '<span>' + year + '</span>';
        marker.appendChild(label);

        beforeEl.parentNode.insertBefore(marker, beforeEl);
    }

    function computeGap(dateA, dateB) {
        var a = new Date(dateA);
        var b = new Date(dateB);
        var diffDays = (b - a) / (1000 * 60 * 60 * 24);
        if (diffDays <= 0) return 0;
        if (diffDays <= 7) return 2;
        if (diffDays <= 30) return 6;
        if (diffDays <= 90) return 12;
        if (diffDays <= 365) return 20;
        return Math.min(60, 20 + Math.log2(diffDays / 365) * 12);
    }

    // ---- Column toggle ----
    document.querySelectorAll('.column-toggle').forEach(function(btn) {
        btn.addEventListener('click', function() {
            var catId = parseInt(this.dataset.categoryId, 10);
            var visCount = 0;
            for (var k in visibleCategories) { if (visibleCategories[k]) visCount++; }
            if (visibleCategories[catId] && visCount <= 1) return;
            visibleCategories[catId] = !visibleCategories[catId];
            this.classList.toggle('active');
            layoutEvents();
        });
    });

    // ---- Tag filter ----
    document.querySelectorAll('.tag-toggle').forEach(function(btn) {
        btn.addEventListener('click', function() {
            var tagId = parseInt(this.dataset.tagId, 10);
            if (activeTag === tagId) {
                activeTag = 0;
                this.classList.remove('active');
            } else {
                document.querySelectorAll('.tag-toggle.active').forEach(function(b) { b.classList.remove('active'); });
                activeTag = tagId;
                this.classList.add('active');
            }
            reloadTimeline();
        });
    });

    function reloadTimeline() {
        reachedTop = false;
        reachedBottom = false;
        var url = '/t/' + slug + '/events?direction=forward&cursor_date=1000-01-01&cursor_sort=-1&limit=50' +
            (activeTag ? '&tag=' + activeTag : '');
        fetch(url)
            .then(function(resp) { return resp.text(); })
            .then(function(html) {
                eventList.querySelectorAll('.tl-year-marker, .tl-gap').forEach(function(el) { el.remove(); });
                eventList.querySelectorAll('.tl-event').forEach(function(el) { el.remove(); });
                var temp = document.createElement('div');
                temp.innerHTML = html.trim();
                temp.querySelectorAll('.tl-event').forEach(function(e) { eventList.appendChild(e); });
                layoutEvents();
                window.scrollTo(0, 0);
            })
            .catch(function(err) { console.error('reload:', err); });
    }

    // ---- Infinite scroll ----
    function getFirstEvent() { return eventList.querySelector('.tl-event'); }
    function getLastEvent() {
        var evts = eventList.querySelectorAll('.tl-event');
        return evts.length > 0 ? evts[evts.length - 1] : null;
    }
    function getCursor(el) {
        if (!el) return { date: '', sort: '0' };
        return { date: el.dataset.date, sort: el.dataset.sort || '0' };
    }

    function loadMore(direction) {
        if (loading) return;
        if (direction === 'forward' && reachedBottom) return;
        if (direction === 'backward' && reachedTop) return;
        var cursor = direction === 'forward' ? getCursor(getLastEvent()) : getCursor(getFirstEvent());
        if (!cursor.date) return;
        loading = true;
        var url = '/t/' + slug + '/events?direction=' + direction +
            '&cursor_date=' + cursor.date + '&cursor_sort=' + cursor.sort + '&limit=50' +
            (activeTag ? '&tag=' + activeTag : '');
        fetch(url)
            .then(function(resp) { return resp.text(); })
            .then(function(html) {
                var trimmed = html.trim();
                if (!trimmed) {
                    if (direction === 'forward') reachedBottom = true; else reachedTop = true;
                    return;
                }
                eventList.querySelectorAll('.tl-year-marker, .tl-gap').forEach(function(el) { el.remove(); });
                var temp = document.createElement('div');
                temp.innerHTML = trimmed;
                var newEvents = temp.querySelectorAll('.tl-event');
                if (newEvents.length === 0) {
                    if (direction === 'forward') reachedBottom = true; else reachedTop = true;
                    return;
                }
                if (direction === 'forward') {
                    newEvents.forEach(function(e) { eventList.appendChild(e); });
                } else {
                    var hBefore = eventList.scrollHeight;
                    for (var i = newEvents.length - 1; i >= 0; i--) eventList.insertBefore(newEvents[i], eventList.firstChild);
                    window.scrollBy(0, eventList.scrollHeight - hBefore);
                }
                layoutEvents();
                pruneIfNeeded();
            })
            .catch(function(err) { console.error('load events:', err); })
            .finally(function() { loading = false; });
    }

    function pruneIfNeeded() {
        var evts = eventList.querySelectorAll('.tl-event');
        if (evts.length <= 500) return;
        var viewMid = window.scrollY + window.innerHeight / 2;
        var midIdx = 0, minDist = Infinity;
        evts.forEach(function(el, i) {
            var d = Math.abs(el.getBoundingClientRect().top + window.scrollY + el.offsetHeight / 2 - viewMid);
            if (d < minDist) { minDist = d; midIdx = i; }
        });
        var keepStart = Math.max(0, midIdx - 250);
        var keepEnd = Math.min(evts.length, midIdx + 250);
        if (keepEnd < evts.length) { for (var i = evts.length - 1; i >= keepEnd; i--) evts[i].remove(); reachedBottom = false; }
        if (keepStart > 0) {
            var hBefore = eventList.scrollHeight;
            for (var j = keepStart - 1; j >= 0; j--) evts[j].remove();
            window.scrollBy(0, -(hBefore - eventList.scrollHeight));
            reachedTop = false;
        }
    }

    // Observers
    var obsOpts = { root: null, rootMargin: '400px', threshold: 0 };
    new IntersectionObserver(function(entries) {
        entries.forEach(function(e) { if (e.isIntersecting) loadMore('backward'); });
    }, obsOpts).observe(sentinelTop);
    new IntersectionObserver(function(entries) {
        entries.forEach(function(e) { if (e.isIntersecting) loadMore('forward'); });
    }, obsOpts).observe(sentinelBottom);

    // Reposition axis stripe on resize
    window.addEventListener('resize', function() {
        requestAnimationFrame(function() { positionAxisStripe(computeGrid()); });
    });

    // ---- Inline event expansion ----
    document.addEventListener('click', function(e) {
        var title = e.target.closest('.tl-event-title');
        if (!title) return;
        e.preventDefault();

        var evtRow = title.closest('.tl-event');
        if (!evtRow) return;

        // If already expanded, collapse
        var existing = evtRow.querySelector('.evt-inline-panel');
        if (existing) {
            existing.remove();
            evtRow.classList.remove('evt-expanded');
            return;
        }

        // Close any other open panel
        var openPanel = eventList.querySelector('.evt-inline-panel');
        if (openPanel) {
            openPanel.closest('.tl-event').classList.remove('evt-expanded');
            openPanel.remove();
        }

        var url = title.dataset.inlineUrl;
        if (!url) return;

        evtRow.classList.add('evt-expanded');
        fetch(url)
            .then(function(resp) { return resp.text(); })
            .then(function(html) {
                var panel = document.createElement('div');
                panel.className = 'evt-inline-panel';
                panel.innerHTML = html.trim();
                evtRow.appendChild(panel);
            })
            .catch(function(err) {
                console.error('inline load:', err);
                evtRow.classList.remove('evt-expanded');
            });
    });

    // ---- Density minimap ----
    function loadMinimap() {
        var minimap = document.getElementById('density-minimap');
        if (!minimap) return;

        fetch('/t/' + slug + '/density')
            .then(function(resp) { return resp.json(); })
            .then(function(buckets) {
                if (!buckets || buckets.length === 0) return;

                var maxCount = 0;
                buckets.forEach(function(b) { if (b.count > maxCount) maxCount = b.count; });

                var barHeight = 14;
                var barGap = 2;
                var maxBarWidth = 40;
                var labelWidth = 30;
                var svgWidth = labelWidth + maxBarWidth + 4;
                var svgHeight = buckets.length * (barHeight + barGap);

                var svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' + svgWidth + '" height="' + svgHeight + '">';
                buckets.forEach(function(b, i) {
                    var y = i * (barHeight + barGap);
                    var w = Math.max(2, (b.count / maxCount) * maxBarWidth);

                    svg += '<text class="density-label" x="' + (labelWidth - 2) + '" y="' + (y + barHeight - 3) +
                        '" text-anchor="end">' + b.label + '</text>';
                    svg += '<rect class="density-bar" x="' + (labelWidth + 2) + '" y="' + y +
                        '" width="' + w + '" height="' + barHeight + '" rx="2" data-date="' + b.date + '">' +
                        '<title>' + b.label + ': ' + b.count + ' events</title></rect>';
                });
                svg += '</svg>';
                minimap.innerHTML = svg;

                // Click to jump
                minimap.querySelectorAll('.density-bar').forEach(function(bar) {
                    bar.addEventListener('click', function() {
                        var date = this.dataset.date;
                        reachedTop = false;
                        reachedBottom = false;
                        var url = '/t/' + slug + '/events?direction=forward&cursor_date=' + date + '&cursor_sort=-1&limit=50' +
                            (activeTag ? '&tag=' + activeTag : '');
                        fetch(url)
                            .then(function(resp) { return resp.text(); })
                            .then(function(html) {
                                eventList.querySelectorAll('.tl-year-marker, .tl-gap, .tl-event').forEach(function(el) { el.remove(); });
                                var temp = document.createElement('div');
                                temp.innerHTML = html.trim();
                                temp.querySelectorAll('.tl-event').forEach(function(e) { eventList.appendChild(e); });
                                layoutEvents();
                                window.scrollTo(0, container.querySelector('.tl-body').offsetTop);
                            });
                    });
                });
            })
            .catch(function(err) { console.error('minimap:', err); });
    }

    // Initial
    layoutEvents();
    loadMinimap();
})();
