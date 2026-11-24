// Period picker for the transactions table. Every row has one button carrying
// its current period; clicking it opens this single shared popover, where the
// user switches between month, quarter and year and picks one. The choice is
// posted as a plain form to /transactions/{id}/period ("2026-03", "2026-Q1" or
// "2026"), which redirects back so every total reflects the new window.
(() => {
    const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const MODES = [['month', 'Month'], ['quarter', 'Quarter'], ['year', 'Year']];
    const COLS = { month: 3, quarter: 2, year: 3 };

    const el = document.createElement('div');
    el.className = 'period-picker';
    el.setAttribute('role', 'dialog');
    el.setAttribute('aria-label', 'Choose period');
    el.hidden = true;
    el.innerHTML = `
        <div class="pp-modes" role="group" aria-label="Period type">
            ${MODES.map(([m, label]) => `<button type="button" data-mode="${m}">${label}</button>`).join('')}
        </div>
        <div class="pp-nav">
            <button type="button" class="pp-step" data-step="-1" aria-label="Previous">‹</button>
            <strong class="pp-title" aria-live="polite"></strong>
            <button type="button" class="pp-step" data-step="1" aria-label="Next">›</button>
        </div>
        <div class="pp-grid"></div>
        <button type="button" class="pp-reset"></button>`;
    document.body.appendChild(el);

    const grid = el.querySelector('.pp-grid');
    const title = el.querySelector('.pp-title');
    const reset = el.querySelector('.pp-reset');

    // trigger: the row button that opened the picker. year: the year shown in
    // month and quarter mode. page: the first of the twelve years in year mode.
    let trigger = null, mode = 'month', year = 0, page = 0, ownYear = 0, ownMonth = 0;

    const pad = (n) => String(n).padStart(2, '0');

    function cells() {
        if (mode === 'month') {
            return MONTHS.map((name, i) => ({
                value: `${year}-${pad(i + 1)}`, label: name,
                own: year === ownYear && i + 1 === ownMonth,
            }));
        }
        if (mode === 'quarter') {
            return [1, 2, 3, 4].map((q) => ({
                value: `${year}-Q${q}`, label: `Q${q}`,
                hint: `${MONTHS[3 * q - 3]}–${MONTHS[3 * q - 1]}`,
                own: year === ownYear && Math.ceil(ownMonth / 3) === q,
            }));
        }
        return Array.from({ length: 12 }, (_, i) => ({
            value: String(page + i), label: String(page + i), own: page + i === ownYear,
        }));
    }

    function render() {
        el.querySelectorAll('[data-mode]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.mode === mode)));
        title.textContent = mode === 'year' ? `${page} – ${page + 11}` : String(year);
        grid.className = `pp-grid pp-${mode}`;
        const current = trigger.dataset.period;
        grid.innerHTML = cells().map((c) => `
            <button type="button" class="pp-cell${c.own ? ' own' : ''}" data-value="${c.value}"
                aria-pressed="${c.value === current}"${c.own ? ' title="When the transaction happened"' : ''}>
                ${c.label}${c.hint ? `<small>${c.hint}</small>` : ''}
            </button>`).join('');
        // Month mode is a row taller than quarter mode; re-place the popover
        // so a switch never lets it grow over its own button.
        if (!el.hidden) position();
    }


    function position() {
        const r = trigger.getBoundingClientRect();
        const w = el.offsetWidth, h = el.offsetHeight, gap = 6, margin = 8;
        let left = Math.min(r.left, document.documentElement.clientWidth - w - margin);
        left = Math.max(margin, left);
        let top = r.bottom + gap;
        if (top + h > window.innerHeight - margin && r.top - gap - h > margin) top = r.top - gap - h;
        el.style.left = `${left + window.scrollX}px`;
        el.style.top = `${top + window.scrollY}px`;
    }

    function open(btn) {
        trigger = btn;
        const [sy] = btn.dataset.start.split('-').map(Number);
        [ownYear, ownMonth] = btn.dataset.own.split('-').map(Number);
        mode = btn.dataset.kind || 'month';
        year = sy;
        // More past than future on the year page: most reassignments look back.
        page = sy - 7;
        const ownLabel = `${MONTHS[ownMonth - 1]} ${ownYear}`;
        const ownValue = `${ownYear}-${pad(ownMonth)}`;
        reset.hidden = btn.dataset.period === ownValue;
        reset.dataset.value = ownValue;
        reset.textContent = `Back to ${ownLabel}`;
        render();
        el.hidden = false;
        btn.setAttribute('aria-expanded', 'true');
        position();
        (grid.querySelector('[aria-pressed="true"]') || grid.querySelector('.own') || grid.firstElementChild).focus();
    }

    function close(restoreFocus) {
        if (!trigger) return;
        el.hidden = true;
        trigger.setAttribute('aria-expanded', 'false');
        if (restoreFocus) trigger.focus();
        trigger = null;
    }

    function choose(value) {
        if (value === trigger.dataset.period) return close(true);
        const form = document.createElement('form');
        form.method = 'post';
        form.action = `/transactions/${trigger.dataset.periodId}/period`;
        const input = document.createElement('input');
        input.type = 'hidden';
        input.name = 'period';
        input.value = value;
        form.appendChild(input);
        document.body.appendChild(form);
        el.querySelectorAll('button').forEach((b) => { b.disabled = true; });
        form.submit();
    }

    document.addEventListener('click', (e) => {
        const btn = e.target.closest('.period-btn');
        if (btn) {
            const wasOpen = trigger === btn;
            close(false);
            if (!wasOpen) open(btn);
            return;
        }
        if (trigger && !el.contains(e.target)) close(false);
    });

    el.addEventListener('click', (e) => {
        const b = e.target.closest('button');
        if (!b) return;
        if (b.dataset.mode) {
            mode = b.dataset.mode;
            if (mode === 'year' && (year < page || year > page + 11)) page = year - 7;
            render();
        } else if (b.dataset.step) {
            if (mode === 'year') page += 12 * Number(b.dataset.step);
            else year += Number(b.dataset.step);
            render();
        } else if (b.dataset.value) {
            choose(b.dataset.value);
        }
    });

    el.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            return close(true);
        }
        const cell = e.target.closest('.pp-cell');
        const delta = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -COLS[mode], ArrowDown: COLS[mode] }[e.key];
        if (!cell || !delta) return;
        e.preventDefault();
        const all = [...grid.children];
        all[Math.min(all.length - 1, Math.max(0, all.indexOf(cell) + delta))].focus();
    });

    // Leaving the popover with the keyboard closes it, like clicking outside.
    el.addEventListener('focusout', (e) => {
        if (trigger && e.relatedTarget && !el.contains(e.relatedTarget) && e.relatedTarget !== trigger) close(false);
    });

    window.addEventListener('resize', () => close(false));
    // The table scrolls horizontally on narrow screens; keep the popover
    // attached to its button rather than floating over another row.
    document.addEventListener('scroll', () => { if (trigger) position(); }, true);
})();
