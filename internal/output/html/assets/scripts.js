document.addEventListener('DOMContentLoaded', function() {
    // Add event listeners to all sort icons for column sorting
    const sortIcons = document.querySelectorAll('.sort-icon');
    
    sortIcons.forEach((icon, index) => {
        icon.addEventListener('click', function(event) {
            event.stopPropagation(); // Prevent the event from bubbling up to the th
            sortTable(index); // Pass the index of the column that was clicked
        });
    });

    // Other event listeners (e.g., for count cells and clear search) remain unchanged
    const countCells = document.querySelectorAll('#resource-type-counts td:nth-child(2)');
    const clearSearch = document.getElementById('clear-search');
    countCells.forEach(cell => {
        cell.addEventListener('click', function() {
            // Get the resource type from the first column of the same row
            const resourceType = this.previousElementSibling.textContent.trim();
            
            // Set the search input value to the resource type
            const searchInput = document.getElementById('search-input');
            searchInput.value = resourceType;
            clearSearch.style.display = 'inline'; // Show the clear icon
            // Trigger the table filter to show the filtered results
            filterTable();
        });
    });

    // Show the clear search icon when there is text in the input
    const searchInput = document.getElementById('search-input');
    searchInput.addEventListener('input', function() {
        const clearSearch = document.getElementById('clear-search');
        if (this.value) {
            clearSearch.style.display = 'inline'; // Show the clear icon
        } else {
            clearSearch.style.display = 'none'; // Hide the clear icon
        }
    });
});

function scrollToUnusedResources(event, resourceType) {
    event.preventDefault();  // Prevent the default anchor behavior

    // Find the "Unused Resources" section and scroll to it
    const section = document.getElementById('unused-resources');
    if (section) {
        section.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } else {
        console.error("Element with id 'unused-resources' not found.");
    }
}

// The filterTable function which filters the table rows based on the search input
function filterTable() {
    const input = document.getElementById('search-input');
    const filter = input.value.toLowerCase();
    const table = document.getElementById('scan-table');
    const rows = table.getElementsByTagName('tr');
    
    for (let i = 1; i < rows.length; i++) { // Start at 1 to skip the header row
        const cells = rows[i].getElementsByTagName('td');
        let rowVisible = false;
        
        for (let j = 0; j < cells.length; j++) {
            const cell = cells[j];
            if (cell && cell.textContent.toLowerCase().includes(filter)) {
                rowVisible = true;
                break;
            }
        }
        
        // Display the row if it matches the filter, otherwise hide it
        rows[i].style.display = rowVisible ? '' : 'none';
    }
}

// The clearSearch function which clears the input and shows all rows
function clearSearch() {
    const searchInput = document.getElementById('search-input');
    const clearSearch = document.getElementById('clear-search');
    
    searchInput.value = ''; // Clear the input field
    clearSearch.style.display = 'none'; // Hide the clear icon
    
    // Show all rows in the table
    const table = document.getElementById('scan-table');
    const rows = table.getElementsByTagName('tr');
    for (let i = 1; i < rows.length; i++) {
        rows[i].style.display = ''; // Reset the display to show all rows
    }
}

// Sorting table columns
function sortTable(columnIndex) {
    const table = document.querySelector('#scan-table');
    const rows = Array.from(table.rows).slice(1); // Skip the header row
    const ths = table.querySelectorAll('th'); // Get all header cells
    let isAsc = ths[columnIndex].classList.contains('sorted-asc');
    
    // Reset all header icons to the default "↕"
    ths.forEach((th) => {
        th.classList.remove('sorted', 'sorted-asc', 'sorted-desc');
        th.querySelector('.sort-icon').textContent = '↕';
    });

    // Update the clicked column's sort icon
    ths[columnIndex].classList.add('sorted');
    ths[columnIndex].querySelector('.sort-icon').textContent = isAsc ? '▼' : '▲';
    ths[columnIndex].classList.add(isAsc ? 'sorted-desc' : 'sorted-asc');

    // Sort rows based on the selected column
    rows.sort((rowA, rowB) => {
        const cellA = rowA.cells[columnIndex].textContent.trim();
        const cellB = rowB.cells[columnIndex].textContent.trim();

        // Compare the values depending on column type
        return (isAsc ? cellA.localeCompare(cellB) : cellB.localeCompare(cellA));
    });

    // Reattach sorted rows
    rows.forEach((row) => table.appendChild(row));
}

// Toggle details visibility
function toggleDetails(button, detailsId) {
    const details = document.getElementById(detailsId);
    const isCollapsed = details.dataset.collapsed === "true";
    details.dataset.collapsed = !isCollapsed;

    const text = button.querySelector('.text');
    const icon = button.querySelector('.icon');

    if (isCollapsed) {
        details.innerHTML = details.dataset.fullText;
        text.textContent = 'Show less';
    } else {
        details.innerHTML = details.dataset.shortText;
        text.textContent = 'Show more';
    }

    // Toggle the expanded class to rotate the icon
    button.classList.toggle('expanded');
}

// Export table data to CSV
function exportTableToCSV(filename) {
    const table = document.getElementById('scan-table');
    const rows = table.querySelectorAll('tr');
    const csvData = [];

    // Loop through rows and cells to create CSV rows
    rows.forEach((row, index) => {
        const cells = row.querySelectorAll('td, th');
        const rowData = [];
        
        cells.forEach(cell => {
            // Check if the cell contains a div with details, exclude the button
            const detailsDiv = cell.querySelector('div');
            if (detailsDiv) {
                // Use the full text details from the div without the button
                rowData.push(`"${detailsDiv.getAttribute('data-full-text').replace(/<br>/g, "\n").trim()}"`);
            } else {
                // Otherwise, just push the cell's text
                rowData.push(`"${cell.innerText.trim().replace(/[⬇⬍⬆]/g, "")}"`);
            }
        });
        
        if (rowData.length) {
            csvData.push(rowData.join(','));
        }
    });

    // Create a CSV string from the data
    const csvString = csvData.join('\n');

    // Trigger a download
    const link = document.createElement('a');
    link.setAttribute('href', 'data:text/csv;charset=utf-8,' + encodeURIComponent(csvString));
    link.setAttribute('download', filename);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
}

// Convert UTC time to the user's local timezone and display
function convertToLocalTime(utcTimeString) {
    const utcDate = new Date(utcTimeString);
    const localDate = utcDate.toLocaleString(); // Convert to local timezone format
    return localDate;
}

// Set the generated report time in the local timezone
function displayLocalReportTime() {
    const reportTimeElement = document.getElementById('generated-time');
    
    if (reportTimeElement) {
        const utcTime = reportTimeElement.getAttribute('data-utc-time');
        const localTime = convertToLocalTime(utcTime * 1000); // Convert seconds to milliseconds
        reportTimeElement.textContent = `${localTime}`;
    }
}

// Add tooltips for "N/A"
function addTooltipsForNA() {
    const cells = document.querySelectorAll('td');
    cells.forEach(cell => {
        if (cell.textContent.trim() === 'N/A') {
            cell.classList.add('tooltip');
            const tooltipSpan = document.createElement('span');
            tooltipSpan.classList.add('tooltiptext');
            tooltipSpan.textContent = 'This resource does not have a cost calculation available';
            cell.appendChild(tooltipSpan);
        }
    });
}

// Call the function on document load
document.addEventListener("DOMContentLoaded", addTooltipsForNA);
