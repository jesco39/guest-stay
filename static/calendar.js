document.addEventListener('DOMContentLoaded', function() {
    let checkIn = null;
    let checkOut = null;
    const days = document.querySelectorAll('.cal-day[data-date]');
    const selection = document.getElementById('selection');
    const selCheckIn = document.getElementById('sel-checkin');
    const selCheckOut = document.getElementById('sel-checkout');
    const bookLink = document.getElementById('book-link');

    days.forEach(function(day) {
        day.addEventListener('click', function() {
            const date = this.getAttribute('data-date');

            if (!checkIn || (checkIn && checkOut)) {
                // First click or reset
                checkIn = date;
                checkOut = null;
                clearHighlights();
                this.classList.add('selected');
                selection.style.display = 'none';
            } else {
                // Second click
                if (date <= checkIn) {
                    // Clicked before check-in, reset
                    checkIn = date;
                    checkOut = null;
                    clearHighlights();
                    this.classList.add('selected');
                    selection.style.display = 'none';
                    return;
                }

                // Check if any blocked date in range
                if (hasBlockedInRange(checkIn, date)) {
                    checkIn = date;
                    checkOut = null;
                    clearHighlights();
                    this.classList.add('selected');
                    return;
                }

                checkOut = date;
                highlightRange(checkIn, checkOut);

                selCheckIn.textContent = checkIn;
                selCheckOut.textContent = checkOut;
                bookLink.href = '/book?check_in=' + checkIn + '&check_out=' + checkOut;
                selection.style.display = 'block';
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
