document.addEventListener('DOMContentLoaded', function() {
    var checkIn = null;
    var checkOut = null;

    var selection = document.getElementById('selection');
    var selCheckIn = document.getElementById('sel-checkin');
    var selCheckOut = document.getElementById('sel-checkout');
    var bookLink = document.getElementById('book-link');
    var sentinel = document.getElementById('cal-sentinel');

    // Scroll today into view on load
    if (sentinel) {
        var todayStr = sentinel.dataset.today;
        var todayCell = document.querySelector('.cal-day[data-date="' + todayStr + '"]');
        if (todayCell) {
            todayCell.scrollIntoView({ block: 'nearest' });
        }
    }

    // Event delegation handles both initial and lazy-loaded day cells
    var card = document.querySelector('.card');
    card.addEventListener('click', function(e) {
        var day = e.target.closest('.cal-day[data-date]');
        if (!day || day.classList.contains('blocked') || day.classList.contains('past') || day.classList.contains('empty')) return;

        var date = day.getAttribute('data-date');

        if (!checkIn || (checkIn && checkOut)) {
            // First click or reset after complete selection
            checkIn = date;
            checkOut = null;
            clearHighlights();
            day.classList.add('selected');
            selCheckIn.textContent = checkIn;
            selCheckOut.textContent = '(select checkout)';
            bookLink.style.display = 'none';
            selection.style.display = 'block';
        } else {
            if (date <= checkIn) {
                // Clicked before or on check-in — restart
                checkIn = date;
                checkOut = null;
                clearHighlights();
                day.classList.add('selected');
                selCheckIn.textContent = checkIn;
                selCheckOut.textContent = '(select checkout)';
                bookLink.style.display = 'none';
                return;
            }

            if (hasBlockedInRange(checkIn, date)) {
                // Blocked date in range — treat second click as new check-in
                checkIn = date;
                checkOut = null;
                clearHighlights();
                day.classList.add('selected');
                selCheckIn.textContent = checkIn;
                selCheckOut.textContent = '(select checkout)';
                bookLink.style.display = 'none';
                return;
            }

            checkOut = date;
            highlightRange(checkIn, checkOut);
            selCheckIn.textContent = checkIn;
            selCheckOut.textContent = checkOut;
            bookLink.href = '/book?check_in=' + checkIn + '&check_out=' + checkOut;
            bookLink.style.display = '';
            selection.style.display = 'block';
        }
    });

    function clearHighlights() {
        document.querySelectorAll('.cal-day.selected, .cal-day.in-range').forEach(function(d) {
            d.classList.remove('selected', 'in-range');
        });
    }

    function highlightRange(start, end) {
        clearHighlights();
        document.querySelectorAll('.cal-day[data-date]').forEach(function(d) {
            var date = d.getAttribute('data-date');
            if (date === start || date === end) {
                d.classList.add('selected');
            } else if (date > start && date < end) {
                d.classList.add('in-range');
            }
        });
    }

    function hasBlockedInRange(start, end) {
        var blocked = document.querySelectorAll('.cal-day.blocked[data-date]');
        for (var i = 0; i < blocked.length; i++) {
            var date = blocked[i].getAttribute('data-date');
            if (date > start && date < end) return true;
        }
        return false;
    }

    // Infinite scroll: load more months as user approaches the bottom
    if (sentinel && window.IntersectionObserver) {
        var MAX_MONTHS = 24;
        var monthsLoaded = 3;
        var fetching = false;

        var observer = new IntersectionObserver(function(entries) {
            if (!entries[0].isIntersecting || fetching || monthsLoaded >= MAX_MONTHS) return;

            var nextMonth = sentinel.dataset.nextMonth;
            if (!nextMonth) return;

            fetching = true;
            fetch('/calendar/month?m=' + nextMonth)
                .then(function(res) { return res.text(); })
                .then(function(html) {
                    sentinel.insertAdjacentHTML('beforebegin', html);

                    // Advance sentinel: parse "YYYY-MM", add 1 month using JS Date
                    var parts = nextMonth.split('-').map(Number);
                    // parts[1] is 1-indexed month; Date(year, month, 1) treats month as 0-indexed,
                    // so passing the 1-indexed value directly advances by one month.
                    var next = new Date(parts[0], parts[1], 1);
                    sentinel.dataset.nextMonth = next.getFullYear() + '-' +
                        String(next.getMonth() + 1).padStart(2, '0');
                    monthsLoaded++;
                    fetching = false;
                })
                .catch(function() { fetching = false; });
        }, { rootMargin: '200px' });

        observer.observe(sentinel);
    }
});
