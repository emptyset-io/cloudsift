// Initialize charts when the document loads
document.addEventListener('DOMContentLoaded', function() {
    initializeCharts();
    initializeSortableTables();
    addTooltipsForNA();
    setupModalListeners();
    setupCostPeriodSelector();
    displayLocalReportTime();
    convertTimestamps();
    initializeSearch();
});

let costChart = null;

// Chart initialization
function initializeCharts() {
    // Resource Distribution Chart
    const resourceCtx = document.getElementById('resourceDistributionChart').getContext('2d');
    const resourceData = getResourceTypeData();
    new Chart(resourceCtx, {
        type: 'doughnut',
        data: {
            labels: resourceData.labels,
            datasets: [{
                data: resourceData.data,
                backgroundColor: generateColors(resourceData.labels.length),
                borderColor: 'white',
                borderWidth: 2
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    position: 'right',
                    labels: {
                        color: '#1d1d1f',
                        font: {
                            family: 'Inter'
                        }
                    }
                }
            }
        }
    });

    // Initialize Cost Breakdown Chart
    updateCostChart('hourly');
}

// Get data for resource type distribution chart
function getResourceTypeData() {
    const table = document.getElementById('resource-type-counts');
    if (!table) return { labels: [], data: [] };

    const rows = table.getElementsByTagName('tr');
    const labels = [];
    const data = [];

    for (let i = 1; i < rows.length; i++) {
        const cells = rows[i].getElementsByTagName('td');
        if (cells.length >= 2) {
            labels.push(cells[0].textContent.trim());
            data.push(parseInt(cells[1].textContent.trim()) || 0);
        }
    }

    return { labels, data };
}

// Get data for cost breakdown chart based on period
function getCostData(period) {
    const table = document.getElementById('combined-costs');
    if (!table) return { labels: [], data: [] };

    const rows = table.getElementsByTagName('tr');
    const labels = [];
    const data = [];
    
    // Map period to column index
    const columnMap = {
        'hourly': 1,
        'daily': 2,
        'monthly': 3,
        'yearly': 4,
        'lifetime': 5
    };
    
    const columnIndex = columnMap[period];

    for (let i = 1; i < rows.length; i++) {
        const cells = rows[i].getElementsByTagName('td');
        if (cells.length > columnIndex) {
            const resourceType = cells[0].textContent.trim();
            const costText = cells[columnIndex].textContent.replace(/[$,]/g, '').trim();
            const cost = parseFloat(costText);
            
            if (!isNaN(cost) && cost > 0) {
                labels.push(resourceType);
                data.push(cost);
            }
        }
    }

    // Sort by value descending
    const indices = data.map((_, i) => i);
    indices.sort((a, b) => data[b] - data[a]);
    
    return {
        labels: indices.map(i => labels[i]),
        data: indices.map(i => data[i])
    };
}

// Update cost chart with new period data
function updateCostChart(period) {
    const costCtx = document.getElementById('costBreakdownChart');
    if (!costCtx) return;

    const costData = getCostData(period);
    
    if (costChart) {
        costChart.destroy();
    }
    
    const periodLabel = period.charAt(0).toUpperCase() + period.slice(1);
    
    costChart = new Chart(costCtx, {
        type: 'bar',
        data: {
            labels: costData.labels,
            datasets: [{
                label: `${periodLabel} Cost ($)`,
                data: costData.data,
                backgroundColor: 'rgba(60, 52, 156, 0.7)',
                borderColor: 'rgb(60, 52, 156)',
                borderWidth: 1
            }]
        },
        options: {
            indexAxis: 'y',
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    display: false
                },
                tooltip: {
                    callbacks: {
                        label: (context) => {
                            const value = context.raw;
                            return `$${value.toLocaleString(undefined, {
                                minimumFractionDigits: 2,
                                maximumFractionDigits: 2
                            })}`;
                        }
                    }
                }
            },
            scales: {
                x: {
                    beginAtZero: true,
                    grid: {
                        color: 'rgba(0, 0, 0, 0.1)'
                    },
                    ticks: {
                        callback: (value) => {
                            return '$' + value.toLocaleString(undefined, {
                                minimumFractionDigits: 2,
                                maximumFractionDigits: 2
                            });
                        },
                        color: '#1d1d1f',
                        font: {
                            family: 'Inter'
                        }
                    }
                },
                y: {
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: '#1d1d1f',
                        font: {
                            family: 'Inter'
                        }
                    }
                }
            }
        }
    });
}

// Setup cost period selector
function setupCostPeriodSelector() {
    const buttons = document.querySelectorAll('.cost-period-btn');
    buttons.forEach(button => {
        button.addEventListener('click', () => {
            // Update active state
            buttons.forEach(b => b.classList.remove('active'));
            button.classList.add('active');
            
            // Update chart
            updateCostChart(button.dataset.period);
        });
    });
}

// Generate colors for the chart
function generateColors(count) {
    const baseColor = [60, 52, 156]; // RGB values for the accent color
    const colors = [];
    
    for (let i = 0; i < count; i++) {
        const opacity = 0.4 + (0.5 * (i / count));
        colors.push(`rgba(${baseColor[0]}, ${baseColor[1]}, ${baseColor[2]}, ${opacity})`);
    }
    
    return colors;
}

// Sortable tables
function initializeSortableTables() {
    const tables = document.querySelectorAll('table');
    tables.forEach(table => {
        const headers = table.querySelectorAll('th');
        headers.forEach((header, index) => {
            if (header.querySelector('.sort-icon')) {
                header.addEventListener('click', () => sortTable(table, index));
            }
        });
    });
}

function sortTable(table, column) {
    const tbody = table.querySelector('tbody');
    const rows = Array.from(tbody.querySelectorAll('tr'));
    const headers = table.querySelectorAll('th');
    const currentHeader = headers[column];
    
    // Toggle sort direction
    const isAscending = !currentHeader.classList.contains('sorted-asc');
    
    // Remove sorted classes from all headers
    headers.forEach(header => {
        header.classList.remove('sorted-asc', 'sorted-desc');
    });
    
    // Add appropriate class to current header
    currentHeader.classList.add(isAscending ? 'sorted-asc' : 'sorted-desc');

    // Sort the rows
    rows.sort((a, b) => {
        const aValue = a.cells[column].textContent.trim();
        const bValue = b.cells[column].textContent.trim();
        
        // Check if the values are numbers (including currency)
        const aNum = parseFloat(aValue.replace(/[^0-9.-]+/g, ''));
        const bNum = parseFloat(bValue.replace(/[^0-9.-]+/g, ''));
        
        if (!isNaN(aNum) && !isNaN(bNum)) {
            return isAscending ? aNum - bNum : bNum - aNum;
        }
        
        return isAscending ? 
            aValue.localeCompare(bValue) : 
            bValue.localeCompare(aValue);
    });
    
    // Reorder the rows
    rows.forEach(row => tbody.appendChild(row));
}

// Search functionality
function initializeSearch() {
    const searchInput = document.getElementById('search-input');
    const clearButton = document.getElementById('clear-search');
    
    if (searchInput) {
        searchInput.addEventListener('input', () => {
            filterTable();
            clearButton.style.display = searchInput.value ? 'inline' : 'none';
        });
    }
}

function filterTable() {
    const input = document.getElementById('search-input');
    const filter = input.value.toLowerCase();
    const table = document.getElementById('scan-table');
    const rows = table.getElementsByTagName('tr');
    
    for (let i = 1; i < rows.length; i++) {
        const cells = rows[i].getElementsByTagName('td');
        let rowVisible = false;
        
        for (let cell of cells) {
            if (cell.textContent.toLowerCase().includes(filter)) {
                rowVisible = true;
                break;
            }
        }
        
        rows[i].style.display = rowVisible ? '' : 'none';
    }
}

function clearSearch() {
    const searchInput = document.getElementById('search-input');
    const clearButton = document.getElementById('clear-search');
    
    searchInput.value = '';
    clearButton.style.display = 'none';
    filterTable();
}

// Scroll to resource section
function scrollToUnusedResources(event, resourceType) {
    event.preventDefault();
    
    const section = document.getElementById('unused-resources');
    const searchInput = document.getElementById('search-input');
    
    if (section && searchInput) {
        section.scrollIntoView({ behavior: 'smooth', block: 'start' });
        
        // After scrolling, set the search filter
        setTimeout(() => {
            searchInput.value = resourceType;
            searchInput.focus();
            filterTable();
            document.getElementById('clear-search').style.display = 'inline';
        }, 500);
    }
}

// Modal Functions
function showDetailsModal(resourceId, resourceType, details) {
    const modal = document.getElementById('details-modal');
    const overlay = document.getElementById('details-modal-overlay');
    const modalTitle = modal.querySelector('.modal-title');
    const modalDetails = document.getElementById('modal-details');

    try {
        const formattedDetails = JSON.stringify(JSON.parse(details), null, 2);
        modalTitle.innerHTML = `
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                <polyline points="14 2 14 8 20 8"/>
                <line x1="16" y1="13" x2="8" y2="13"/>
                <line x1="16" y1="17" x2="8" y2="17"/>
                <polyline points="10 9 9 9 8 9"/>
            </svg>
            ${resourceType} Details
        `;
        modalDetails.textContent = formattedDetails;
    } catch (e) {
        modalDetails.textContent = details;
    }

    modal.style.display = 'block';
    overlay.style.display = 'block';
    document.body.style.overflow = 'hidden';
}

function closeDetailsModal() {
    const modal = document.getElementById('details-modal');
    const overlay = document.getElementById('details-modal-overlay');
    
    modal.style.display = 'none';
    overlay.style.display = 'none';
    document.body.style.overflow = '';
}

function setupModalListeners() {
    // Close modal when clicking outside
    document.getElementById('details-modal-overlay').addEventListener('click', closeDetailsModal);

    // Prevent modal close when clicking inside the modal
    document.getElementById('details-modal').addEventListener('click', (e) => {
        e.stopPropagation();
    });

    // Setup search input listener
    document.getElementById('search-input').addEventListener('input', filterTable);
}

// Add tooltips for N/A values
function addTooltipsForNA() {
    const tooltips = document.querySelectorAll('.tooltip');
    tooltips.forEach(tooltip => {
        const tooltipText = tooltip.querySelector('.tooltiptext');
        if (tooltipText) {
            tooltip.addEventListener('mouseenter', () => {
                tooltipText.style.visibility = 'visible';
                tooltipText.style.opacity = '1';
            });
            tooltip.addEventListener('mouseleave', () => {
                tooltipText.style.visibility = 'hidden';
                tooltipText.style.opacity = '0';
            });
        }
    });
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

// Initialize tooltips
function initializeTooltips() {
    const tooltips = document.querySelectorAll('.tooltip');
    tooltips.forEach(tooltip => {
        const text = tooltip.querySelector('.tooltiptext');
        if (text) {
            tooltip.addEventListener('mousemove', (e) => {
                text.style.left = `${e.clientX}px`;
                text.style.top = `${e.clientY - 10}px`;
            });
        }
    });
}

// Convert timestamps to local time
function convertTimestamps() {
    const timestampCells = document.querySelectorAll('td:nth-child(5)'); // Last Used column
    timestampCells.forEach(cell => {
        const timestamp = cell.textContent.trim();
        if (timestamp && timestamp !== 'N/A') {
            const date = new Date(timestamp);
            if (!isNaN(date)) {
                cell.textContent = date.toLocaleString();
            }
        }
    });
}

// Filter resources based on account, region, or resource type
function filterResources(filterType, value) {
    // Get the resources table
    const table = document.getElementById('unused-resources');
    if (!table) return;

    const rows = table.getElementsByTagName('tr');
    let filterColumn;
    
    switch(filterType) {
        case 'account':
            filterColumn = 0; // Account column
            break;
        case 'region':
            filterColumn = 1; // Region column
            break;
        case 'type':
            filterColumn = 2; // Resource Type column
            break;
        default:
            return;
    }

    // Scroll to the resources section
    document.getElementById('unused-resources-section').scrollIntoView({ behavior: 'smooth' });

    // Clear any existing highlights
    for (let i = 1; i < rows.length; i++) {
        rows[i].classList.remove('highlighted');
    }

    // Highlight matching rows
    for (let i = 1; i < rows.length; i++) {
        const cell = rows[i].getElementsByTagName('td')[filterColumn];
        if (cell && cell.textContent.trim() === value) {
            rows[i].classList.add('highlighted');
        }
    }
}
