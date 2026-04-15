document.addEventListener('DOMContentLoaded', function() {
    let checkIn = (typeof serverCheckIn !== 'undefined' && serverCheckIn) ? serverCheckIn : null;
    let checkOut = null;
    const days = document.querySelectorAll('.cal-day[data-date]:not(.blocked):not(.past)');
    const selection = document.getElementById('selection');
    const selCheckIn = document.getElementById('sel-checkin');
    const selCheckOut = document.getElementById('sel-checkout');
    const bookLink = document.getElementById('book-link');
    const navPrev = document.getElementById('nav-prev');
    const navNext = document.getElementById('nav-next');

    // If we have a persisted check-in from a previous month, show the banner
    if (checkIn) {
        selCheckIn.textContent = checkIn;
        selCheckOut.textContent = '(select checkout)';
        bookLink.style.display = 'none';
        selection.style.display = 'block';
    }

    function updateNavLinks() {
        [navPrev, navNext].forEach(function(link) {
            var href = link.href.replace(/&check_in=[^&]*/g, '');
            if (checkIn && !checkOut) {
                href += '&check_in=' + checkIn;
            }
            link.href = href;
        });
    }

    days.forEach(function(day) {
        day.addEventListener('click', function() {
            const date = this.getAttribute('data-date');

            if (!checkIn || (checkIn && checkOut)) {
                // First click or reset
                checkIn = date;
                checkOut = null;
                clearHighlights();
                this.classList.add('selected');
                selCheckIn.textContent = checkIn;
                selCheckOut.textContent = '(select checkout)';
                bookLink.style.display = 'none';
                selection.style.display = 'block';
                updateNavLinks();
            } else {
                // Second click
                if (date <= checkIn) {
                    // Clicked before or on check-in, reset
                    checkIn = date;
                    checkOut = null;
                    clearHighlights();
                    this.classList.add('selected');
                    selCheckIn.textContent = checkIn;
                    selCheckOut.textContent = '(select checkout)';
                    bookLink.style.display = 'none';
                    updateNavLinks();
                    return;
                }

                // Check if any blocked date in range
                if (hasBlockedInRange(checkIn, date)) {
                    checkIn = date;
                    checkOut = null;
                    clearHighlights();
                    this.classList.add('selected');
                    selCheckIn.textContent = checkIn;
                    selCheckOut.textContent = '(select checkout)';
                    bookLink.style.display = 'none';
                    updateNavLinks();
                    return;
                }

                checkOut = date;
                highlightRange(checkIn, checkOut);

                selCheckIn.textContent = checkIn;
                selCheckOut.textContent = checkOut;
                bookLink.href = '/book?check_in=' + checkIn + '&check_out=' + checkOut;
                bookLink.style.display = '';
                selection.style.display = 'block';
                updateNavLinks();
            }
        });
    });

    function clearHighlights() {
        days.forEach(function(d) {
            d.classList.remove('selected', 'in-range');
        });
    }

    function highlightRange(start, end) {
        clearHighlights();
        days.forEach(function(d) {
            const date = d.getAttribute('data-date');
            if (date === start || date === end) {
                d.classList.add('selected');
            } else if (date > start && date < end) {
                d.classList.add('in-range');
            }
        });
    }

    function hasBlockedInRange(start, end) {
        const allDays = document.querySelectorAll('.cal-day');
        for (let i = 0; i < allDays.length; i++) {
            const d = allDays[i];
            if (d.classList.contains('blocked')) {
                const date = d.getAttribute('data-date');
                if (date && date > start && date < end) {
                    return true;
                }
            }
        }
        return false;
    }
});
