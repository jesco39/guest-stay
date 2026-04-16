document.addEventListener('DOMContentLoaded', function() {
    // URL param is authoritative; fall back to sessionStorage for cross-month handoff on iOS
    let checkIn = (typeof serverCheckIn !== 'undefined' && serverCheckIn)
        ? serverCheckIn
        : (sessionStorage.getItem('checkIn') || null);
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
        sessionStorage.setItem('checkIn', checkIn);
        selCheckIn.textContent = checkIn;
        selCheckOut.textContent = '(select checkout)';
        bookLink.style.display = 'none';
        selection.style.display = 'block';
        var checkInCell = document.querySelector('.cal-day[data-date="' + checkIn + '"]');
        if (checkInCell) {
            checkInCell.classList.add('selected');
        }
        updateNavLinks();
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
                sessionStorage.setItem('checkIn', checkIn);
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
                    sessionStorage.setItem('checkIn', checkIn);
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
                    sessionStorage.setItem('checkIn', checkIn);
                    clearHighlights();
                    this.classList.add('selected');
                    selCheckIn.textContent = checkIn;
                    selCheckOut.textContent = '(select checkout)';
                    bookLink.style.display = 'none';
                    updateNavLinks();
                    return;
                }

                checkOut = date;
                sessionStorage.removeItem('checkIn');
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
