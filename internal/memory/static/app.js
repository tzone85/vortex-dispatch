// internal/memory/static/app.js
"use strict";

// -- State ----------------------------------------------------------------
let ws = null;
let timeline = [];
let dateIndex = {};     // date string -> index in timeline
let allDates = [];      // sorted date strings
let calendarMonth = null; // Date object for current calendar view
let reconnectDelay = 1000;

// -- DOM Refs -------------------------------------------------------------
const statusEl      = document.getElementById("connection-status");
const sliderEl      = document.getElementById("timeline-slider");
const dateMinEl     = document.getElementById("date-min");
const dateMaxEl     = document.getElementById("date-max");
const dateCurrentEl = document.getElementById("date-current");
const prevDayBtn    = document.getElementById("prev-day");
const nextDayBtn    = document.getElementById("next-day");
const calGridEl     = document.getElementById("calendar-grid");
const calLabelEl    = document.getElementById("cal-month-label");
const calPrevBtn    = document.getElementById("cal-prev");
const calNextBtn    = document.getElementById("cal-next");
const chartBarEl    = document.getElementById("chart-bar");
const chartDonutEl  = document.getElementById("chart-doughnut");
const searchBoxEl   = document.getElementById("search-box");

// -- Utility: clear all children ------------------------------------------
function clearChildren(el) {
  while (el.firstChild) {
    el.removeChild(el.firstChild);
  }
}

// -- WebSocket ------------------------------------------------------------
function connect() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(proto + "//" + location.host + "/ws");

  ws.onopen = function() {
    statusEl.textContent = "Connected";
    statusEl.className = "connected";
    reconnectDelay = 1000;
  };

  ws.onclose = function() {
    statusEl.textContent = "Disconnected";
    statusEl.className = "disconnected";
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
  };

  ws.onerror = function() {
    ws.close();
  };

  ws.onmessage = function(evt) {
    var msg;
    try { msg = JSON.parse(evt.data); } catch (_) { return; }
    handleMessage(msg);
  };
}

function send(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj));
  }
}

// -- Message Handler ------------------------------------------------------
function handleMessage(msg) {
  switch (msg.type) {
    case "init":
      handleInit(msg);
      break;
    case "day_detail":
      handleDayDetail(msg);
      break;
    case "search_results":
      handleSearchResults(msg);
      break;
  }
}

// -- Init -----------------------------------------------------------------
function handleInit(msg) {
  timeline = msg.timeline || [];
  allDates = timeline.map(function(e) { return e.date; });
  dateIndex = {};
  allDates.forEach(function(d, i) { dateIndex[d] = i; });

  var range = msg.range || {};
  dateMinEl.textContent = range.min || "";
  dateMaxEl.textContent = range.max || "";

  if (allDates.length > 0) {
    sliderEl.min = 0;
    sliderEl.max = allDates.length - 1;
    sliderEl.value = allDates.length - 1;
    selectDateByIndex(allDates.length - 1);
  } else {
    dateCurrentEl.textContent = range.today || "No data";
  }

  // Set calendar to the latest date's month
  if (allDates.length > 0) {
    var last = allDates[allDates.length - 1];
    calendarMonth = new Date(last + "T00:00:00");
  } else {
    calendarMonth = new Date();
  }
  renderCalendar();
  updateCharts();
}

// -- Date Selection -------------------------------------------------------
function selectDateByIndex(idx) {
  if (idx < 0 || idx >= allDates.length) return;
  var date = allDates[idx];
  dateCurrentEl.textContent = date;
  sliderEl.value = idx;
  send({ type: "select_date", date: date });
  renderCalendar(); // update selected highlight
}

function currentDate() {
  var idx = parseInt(sliderEl.value, 10);
  return allDates[idx] || "";
}

sliderEl.addEventListener("input", function() {
  selectDateByIndex(parseInt(this.value, 10));
});

prevDayBtn.addEventListener("click", function() {
  var idx = parseInt(sliderEl.value, 10) - 1;
  if (idx >= 0) selectDateByIndex(idx);
});

nextDayBtn.addEventListener("click", function() {
  var idx = parseInt(sliderEl.value, 10) + 1;
  if (idx < allDates.length) selectDateByIndex(idx);
});

// -- Day Detail -----------------------------------------------------------
function handleDayDetail(msg) {
  renderRunSummary(msg.run_summary);
  renderPRs(msg.prs || []);
  renderFindings(msg.findings || []);
  renderCommits(msg.commits || []);
  updateCharts();
}

function renderRunSummary(summary) {
  var card = document.getElementById("run-summary-card");
  var content = document.getElementById("run-summary-content");
  if (!summary) {
    card.classList.add("hidden");
    return;
  }
  card.classList.remove("hidden");
  clearChildren(content);

  var stats = [
    { label: "Sources Scraped", value: summary.sources_scraped },
    { label: "Findings Total", value: summary.findings_total },
    { label: "Findings Relevant", value: summary.findings_relevant },
    { label: "PRs Created", value: summary.prs_created },
    { label: "Email Sent", value: summary.email_sent ? "Yes" : "No" }
  ];

  stats.forEach(function(s) {
    var div = document.createElement("div");
    div.className = "summary-stat";
    var labelSpan = document.createElement("span");
    labelSpan.className = "label";
    labelSpan.textContent = s.label;
    var valueSpan = document.createElement("span");
    valueSpan.className = "value";
    valueSpan.textContent = s.value;
    div.appendChild(labelSpan);
    div.appendChild(valueSpan);
    content.appendChild(div);
  });
}

function renderPRs(prs) {
  var content = document.getElementById("prs-content");
  document.getElementById("prs-count").textContent = prs.length;
  clearChildren(content);

  if (prs.length === 0) {
    var empty = document.createElement("div");
    empty.className = "empty-state";
    empty.textContent = "No PRs for this date";
    content.appendChild(empty);
    return;
  }

  prs.forEach(function(pr) {
    var row = document.createElement("div");
    row.className = "item-row";

    var title = document.createElement("div");
    title.className = "item-title";
    title.textContent = pr.title;
    row.appendChild(title);

    var meta = document.createElement("div");
    meta.className = "item-meta";
    var link = document.createElement("a");
    link.href = pr.url;
    link.target = "_blank";
    link.rel = "noopener";
    link.textContent = pr.url;
    meta.appendChild(link);
    if (pr.lines > 0) {
      var linesSpan = document.createElement("span");
      linesSpan.textContent = " | " + pr.lines + " lines";
      meta.appendChild(linesSpan);
    }
    if (pr.status) {
      var statusSpan = document.createElement("span");
      statusSpan.textContent = " | " + pr.status;
      meta.appendChild(statusSpan);
    }
    row.appendChild(meta);

    content.appendChild(row);
  });
}

function renderFindings(findings) {
  var content = document.getElementById("findings-content");
  document.getElementById("findings-count").textContent = findings.length;
  clearChildren(content);

  if (findings.length === 0) {
    var empty = document.createElement("div");
    empty.className = "empty-state";
    empty.textContent = "No findings for this date";
    content.appendChild(empty);
    return;
  }

  findings.forEach(function(f) {
    var row = document.createElement("div");
    row.className = "item-row";

    var title = document.createElement("div");
    title.className = "item-title";
    title.textContent = f.title;
    row.appendChild(title);

    var meta = document.createElement("div");
    meta.className = "item-meta";
    var metaText = f.category + " | R:" + f.relevance + " I:" + f.impact + " Risk:" + f.risk + " | " + f.disposition;
    meta.textContent = metaText;
    if (f.source_url) {
      var sep = document.createTextNode(" | ");
      meta.appendChild(sep);
      var link = document.createElement("a");
      link.href = f.source_url;
      link.target = "_blank";
      link.rel = "noopener";
      link.textContent = "source";
      meta.appendChild(link);
    }
    row.appendChild(meta);

    if (f.reasoning) {
      var reasoning = document.createElement("div");
      reasoning.className = "item-reasoning";
      reasoning.textContent = f.reasoning;
      row.appendChild(reasoning);
    }

    content.appendChild(row);
  });
}

function renderCommits(commits) {
  var content = document.getElementById("commits-content");
  document.getElementById("commits-count").textContent = commits.length;
  clearChildren(content);

  if (commits.length === 0) {
    var empty = document.createElement("div");
    empty.className = "empty-state";
    empty.textContent = "No commits for this date";
    content.appendChild(empty);
    return;
  }

  commits.forEach(function(c) {
    var row = document.createElement("div");
    row.className = "item-row";

    var title = document.createElement("div");
    title.className = "item-title";
    title.textContent = c.sha.substring(0, 8) + " " + c.message;
    row.appendChild(title);

    var meta = document.createElement("div");
    meta.className = "item-meta";
    meta.textContent = c.timestamp;
    row.appendChild(meta);

    content.appendChild(row);
  });
}

// -- Search ---------------------------------------------------------------
var searchTimeout = null;
searchBoxEl.addEventListener("input", function() {
  clearTimeout(searchTimeout);
  var query = this.value.trim();
  if (query.length < 2) {
    document.getElementById("search-card").classList.add("hidden");
    return;
  }
  searchTimeout = setTimeout(function() {
    send({ type: "search", query: query, date: currentDate() });
  }, 400);
});

searchBoxEl.addEventListener("keydown", function(e) {
  if (e.key === "Enter") {
    clearTimeout(searchTimeout);
    var query = this.value.trim();
    if (query.length >= 2) {
      send({ type: "search", query: query, date: currentDate() });
    }
  }
});

function handleSearchResults(msg) {
  var card = document.getElementById("search-card");
  var content = document.getElementById("search-content");
  var results = msg.results || [];
  document.getElementById("search-count").textContent = results.length;

  card.classList.remove("hidden");
  clearChildren(content);

  if (results.length === 0) {
    var empty = document.createElement("div");
    empty.className = "empty-state";
    empty.textContent = "No results for \"" + msg.query + "\"";
    content.appendChild(empty);
    return;
  }

  results.forEach(function(r) {
    var item = document.createElement("div");
    item.className = "search-item";

    var header = document.createElement("div");
    var wing = document.createElement("span");
    wing.className = "search-wing";
    wing.textContent = r.wing + (r.room ? " / " + r.room : "");
    header.appendChild(wing);

    var sim = document.createElement("span");
    sim.className = "search-similarity";
    sim.textContent = (r.similarity * 100).toFixed(1) + "%";
    header.appendChild(sim);
    item.appendChild(header);

    if (r.source_file) {
      var src = document.createElement("div");
      src.className = "item-meta";
      src.textContent = "Source: " + r.source_file;
      item.appendChild(src);
    }

    var text = document.createElement("div");
    text.className = "search-text";
    text.textContent = r.text;
    item.appendChild(text);

    content.appendChild(item);
  });
}

// -- Calendar Heatmap -----------------------------------------------------
calPrevBtn.addEventListener("click", function() {
  calendarMonth.setMonth(calendarMonth.getMonth() - 1);
  renderCalendar();
});

calNextBtn.addEventListener("click", function() {
  calendarMonth.setMonth(calendarMonth.getMonth() + 1);
  renderCalendar();
});

function renderCalendar() {
  if (!calendarMonth) return;

  var year = calendarMonth.getFullYear();
  var month = calendarMonth.getMonth();
  var monthNames = [
    "January", "February", "March", "April", "May", "June",
    "July", "August", "September", "October", "November", "December"
  ];
  calLabelEl.textContent = monthNames[month] + " " + year;

  // Build activity lookup for this month
  var activityMap = {};
  timeline.forEach(function(e) {
    activityMap[e.date] = e.activity_level;
  });

  clearChildren(calGridEl);

  // First day of month (0=Sun, 1=Mon, ... 6=Sat)
  var firstDay = new Date(year, month, 1).getDay();
  // Convert to Mon=0 ... Sun=6
  var startOffset = (firstDay + 6) % 7;
  var daysInMonth = new Date(year, month + 1, 0).getDate();
  var selected = currentDate();

  // Empty cells before first day
  for (var i = 0; i < startOffset; i++) {
    var empty = document.createElement("div");
    empty.className = "cal-cell empty level-0";
    calGridEl.appendChild(empty);
  }

  // Day cells
  for (var d = 1; d <= daysInMonth; d++) {
    var dateStr = year + "-" + pad2(month + 1) + "-" + pad2(d);
    var level = activityMap[dateStr] || 0;
    var cell = document.createElement("div");
    cell.className = "cal-cell level-" + level;
    cell.title = dateStr + " (level " + level + ")";
    if (dateStr === selected) {
      cell.classList.add("selected");
    }
    cell.setAttribute("data-date", dateStr);
    cell.addEventListener("click", onCalendarCellClick);
    calGridEl.appendChild(cell);
  }
}

function onCalendarCellClick() {
  var date = this.getAttribute("data-date");
  if (!date) return;
  // If this date exists in timeline, jump slider to it
  if (dateIndex.hasOwnProperty(date)) {
    selectDateByIndex(dateIndex[date]);
  } else {
    // Still request day detail even if not in timeline
    dateCurrentEl.textContent = date;
    send({ type: "select_date", date: date });
    renderCalendar();
  }
}

// -- Charts (QuickChart.io) -----------------------------------------------
function updateCharts() {
  updateBarChart();
  updateDoughnutChart();
}

function updateBarChart() {
  // Show last 7 entries' activity
  var recent = timeline.slice(-7);
  if (recent.length === 0) {
    chartBarEl.src = "";
    return;
  }

  var labels = recent.map(function(e) { return e.date.substring(5); });
  var findingsData = recent.map(function(e) { return e.findings; });
  var prsData = recent.map(function(e) { return e.prs; });

  var config = {
    type: "bar",
    data: {
      labels: labels,
      datasets: [
        { label: "Findings", data: findingsData, backgroundColor: "#58a6ff" },
        { label: "PRs", data: prsData, backgroundColor: "#26a641" }
      ]
    },
    options: {
      plugins: { legend: { labels: { color: "#ccc" } } },
      scales: {
        x: { ticks: { color: "#888" }, grid: { color: "#333" } },
        y: { ticks: { color: "#888" }, grid: { color: "#333" }, beginAtZero: true }
      }
    }
  };

  chartBarEl.src = "https://quickchart.io/chart?w=300&h=180&bkg=%231A1A2E&c=" +
    encodeURIComponent(JSON.stringify(config));
}

function updateDoughnutChart() {
  // Aggregate activity levels across timeline
  var cats = {};
  timeline.forEach(function(e) {
    var key = "Level " + e.activity_level;
    cats[key] = (cats[key] || 0) + 1;
  });

  var labels = Object.keys(cats);
  var data = labels.map(function(k) { return cats[k]; });

  if (labels.length === 0) {
    chartDonutEl.src = "";
    return;
  }

  var colors = ["#16213E", "#0e4429", "#006d32", "#26a641"];

  var config = {
    type: "doughnut",
    data: {
      labels: labels,
      datasets: [{
        data: data,
        backgroundColor: labels.map(function(_, i) { return colors[i % colors.length]; })
      }]
    },
    options: {
      plugins: {
        legend: { labels: { color: "#ccc" } }
      }
    }
  };

  chartDonutEl.src = "https://quickchart.io/chart?w=300&h=180&bkg=%231A1A2E&c=" +
    encodeURIComponent(JSON.stringify(config));
}

// -- Utility --------------------------------------------------------------
function pad2(n) {
  return n < 10 ? "0" + n : "" + n;
}

// -- Keyboard shortcuts ---------------------------------------------------
document.addEventListener("keydown", function(e) {
  // Left/Right arrows for timeline navigation (when not in search box)
  if (document.activeElement === searchBoxEl) return;
  if (e.key === "ArrowLeft") {
    prevDayBtn.click();
    e.preventDefault();
  } else if (e.key === "ArrowRight") {
    nextDayBtn.click();
    e.preventDefault();
  } else if (e.key === "/" || e.key === "s") {
    searchBoxEl.focus();
    e.preventDefault();
  }
});

// -- Boot -----------------------------------------------------------------
connect();
